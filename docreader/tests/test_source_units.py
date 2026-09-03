from docreader.source_units import assemble_source_blocks


def test_assemble_source_blocks_keeps_unicode_ranges_and_separators():
    content, units = assemble_source_blocks(
        [
            {"unit_id": "page-1", "kind": "page", "page": 1, "text": "甲方：甲"},
            {"unit_id": "page-2", "kind": "page", "page": 2, "text": "服务期：一年"},
        ]
    )

    assert content == "甲方：甲\n\n服务期：一年"
    assert units == [
        {
            "unit_id": "page-1",
            "kind": "page",
            "text": "甲方：甲",
            "page": 1,
            "source_start": 0,
            "source_end": 4,
        },
        {
            "unit_id": "page-2",
            "kind": "page",
            "text": "服务期：一年",
            "page": 2,
            "source_start": 6,
            "source_end": 12,
        },
    ]


def test_assemble_source_blocks_adds_table_children_relative_to_parent():
    content, units = assemble_source_blocks(
        [
            {
                "unit_id": "table-1-row-1",
                "kind": "table-row",
                "text": "付款方式 | 每月支付",
                "children": [
                    {"unit_id": "table-1-cell-1", "kind": "table-cell", "source_start": 0, "source_end": 4},
                    {"unit_id": "table-1-cell-2", "kind": "table-cell", "source_start": 7, "source_end": 11},
                ],
            }
        ]
    )

    assert content == "付款方式 | 每月支付"
    assert units[0]["source_start"] == 0
    assert units[0]["source_end"] == len(content)
    assert units[0]["text"] == content
    assert units[1]["parent_id"] == "table-1-row-1"
    assert units[1]["text"] == "付款"
    assert units[1]["source_start"] == 0
    assert units[1]["source_end"] == 4
    assert units[2]["source_start"] == 7
    assert units[2]["source_end"] == len(content)
    assert units[2]["text"] == "每月支付"
