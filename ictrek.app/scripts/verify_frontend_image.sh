#!/usr/bin/env bash
set -euo pipefail

image="${1:?usage: verify_frontend_image.sh IMAGE}"
mkdir -p "$HOME/dev/build/WeKnora"
work_dir="$(mktemp -d "$HOME/dev/build/WeKnora/frontend-image-check.XXXXXX")"
container_id=""
cleanup() {
  if [[ -n "$container_id" ]]; then
    docker rm "$container_id" >/dev/null 2>&1 || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

container_id="$(docker create --network none --entrypoint /bin/true "$image")"
mkdir -p "$work_dir/html"
docker cp "$container_id:/usr/share/nginx/html/." "$work_dir/html"
python3 - "$work_dir/html" <<'PY'
from html.parser import HTMLParser
from pathlib import Path
import sys
from urllib.parse import urljoin, urlsplit

class Assets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        url = attrs.get('src') if tag == 'script' else attrs.get('href') if tag == 'link' else None
        if url and 'assets/' in url and not urlsplit(url).scheme and not url.startswith('//'):
            self.urls.append(url)

root = Path(sys.argv[1]).resolve()
for name in ('index.html', 'embed.html'):
    page = root / name
    assert page.is_file(), f'Missing production entry: {name}'
    parser = Assets()
    parser.feed(page.read_text())
    assert parser.urls, f'No production asset references in {name}'
    for url in parser.urls:
        assert not url.startswith('/'), f'{name}: root-relative asset breaks VOS prefix: {url}'
        asset = (root / urlsplit(url).path).resolve()
        assert root in asset.parents and asset.is_file(), f'{name}: asset missing: {url}'
        for prefix in ('/', '/app/com.ictrek.hybrag/'):
            assert urljoin(prefix, url).startswith(prefix), f'{name}: asset escapes {prefix}: {url}'
    print(f'{name}: verified {len(parser.urls)} relative production assets')
PY
