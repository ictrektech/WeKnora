"""Helpers for preserving source ranges while assembling parsed document text."""

from typing import Any, Dict, Iterable, List, Tuple


def assemble_source_blocks(blocks: Iterable[Dict[str, Any]]) -> Tuple[str, List[Dict[str, Any]]]:
    """Join text blocks and return source-range metadata for each block.

    The Go application stores offsets as Unicode rune offsets with an
    end-exclusive end. Python's ``str`` indexes Unicode code points, which is
    the same unit for valid UTF-8 text. Separators are deliberately kept in
    this helper so the ranges describe the exact assembled text.

    A block may contain ``children`` with local ``source_start`` and
    ``source_end`` offsets. Children are useful for table cells while the
    parent block remains the unit used for page-level fallback.
    """

    parts: List[str] = []
    units: List[Dict[str, Any]] = []
    cursor = 0

    for block_index, block in enumerate(blocks):
        text = str(block.get("text") or "")
        if not text:
            continue

        if parts:
            cursor += 2  # ``\n\n`` between parser blocks
        start = cursor
        end = start + len(text)
        parts.append(text)

        unit_id = str(block.get("unit_id") or f"block-{block_index + 1}")
        unit: Dict[str, Any] = {
            "unit_id": unit_id,
            "kind": str(block.get("kind") or "block"),
            "text": text,
            "source_start": start,
            "source_end": end,
        }
        if block.get("page") is not None:
            unit["page"] = int(block["page"])
        if block.get("parent_id"):
            unit["parent_id"] = str(block["parent_id"])
        units.append(unit)

        for child_index, child in enumerate(block.get("children") or []):
            try:
                child_start = int(child["source_start"])
                child_end = int(child["source_end"])
            except (KeyError, TypeError, ValueError):
                continue
            if child_start < 0 or child_end <= child_start or child_end > len(text):
                continue
            child_unit: Dict[str, Any] = {
                "unit_id": str(child.get("unit_id") or f"{unit_id}-child-{child_index + 1}"),
                "kind": str(child.get("kind") or "block-child"),
                "text": text[child_start:child_end],
                "source_start": start + child_start,
                "source_end": start + child_end,
                "parent_id": unit_id,
            }
            if child.get("page") is not None:
                child_unit["page"] = int(child["page"])
            units.append(child_unit)

        cursor = end

    return "\n\n".join(parts), units
