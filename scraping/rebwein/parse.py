#!/usr/bin/env python3
"""
rebwein/parse.py — parse cached /weine listing pages into clean wine records.

Each `wine-item` card (an <a> linking to /wein/<slug>/<uuid>) carries:
  - <h3 class="wine__title">          → name
  - <div class="wine__description">   → producer (line 1) + vintage (line 2,
                                         a year or "NV")
  - <div class="wine__price">CHF …</div>
  - a `wine__not-available` marker when out of stock

Usage
-----
    nix develop -c python scraping/rebwein/parse.py --from-cache
    nix develop -c python scraping/rebwein/parse.py --from-cache --output scraping/rebwein/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "rebwein" / "cache"
MERCHANT = "rebwein"

_CARD_RE = re.compile(r'<a\s+href="([^"]*/wein/[^"]+)"\s+class="wine-item[^"]*">(.*?)</a>', re.S)
_TITLE_RE = re.compile(r'class="wine__title"[^>]*>(.*?)</h3>', re.S)
_DESC_RE = re.compile(r'class="wine__description"[^>]*>(.*?)</div>', re.S)
_PRICE_RE = re.compile(r'class="wine__price"[^>]*>(.*?)</div>', re.S)


def load_files() -> list[Path]:
    pages = sorted(CACHE_DIR.glob("page_*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return pages


def strip_tags(text: str) -> str:
    return html_module.unescape(re.sub(r"<[^>]+>", "", text or "")).strip()


def to_rappen(raw: str) -> int | None:
    digits = re.sub(r"[^0-9.]", "", strip_tags(raw))
    if not digits or digits == ".":
        return None
    try:
        return round(float(digits) * 100)
    except ValueError:
        return None


def split_description(desc_html: str) -> tuple[str | None, int | None]:
    """description = '<producer><br><vintage|NV>' → (producer, vintage)."""
    lines = [strip_tags(x) for x in re.split(r"<br\s*/?>", desc_html)]
    lines = [x for x in lines if x]
    producer = lines[0] if lines else None
    vintage = None
    for ln in lines[1:] or lines:
        m = re.search(r"\b(19|20)\d{2}\b", ln)
        if m:
            vintage = int(m.group(0))
            break
    return producer, vintage


def card_to_record(url: str, inner: str) -> dict | None:
    m_title = _TITLE_RE.search(inner)
    if not m_title:
        return None
    name = strip_tags(m_title.group(1))
    if not name:
        return None

    m_desc = _DESC_RE.search(inner)
    producer, vintage = split_description(m_desc.group(1)) if m_desc else (None, None)

    m_price = _PRICE_RE.search(inner)
    price = to_rappen(m_price.group(1)) if m_price else None

    url = html_module.unescape(url)
    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": producer,
        "vintage": vintage,
        "currency": "CHF",
        "price": price,
        "in_stock": "wine__not-available" not in inner,
        "sku": url.rstrip("/").rsplit("/", 1)[-1],  # trailing uuid
        "url": url,
    }


def parse_all(pages: list[Path]) -> list[dict]:
    records, seen = [], set()
    for p in pages:
        html = p.read_text(encoding="utf-8")
        page_n = 0
        for url, inner in _CARD_RE.findall(html):
            rec = card_to_record(url, inner)
            if rec is None or rec["url"] in seen:
                continue
            seen.add(rec["url"])
            records.append(rec)
            page_n += 1
        print(f"  [{p.name}] {page_n} product(s)", file=sys.stderr)
    print(f"Total: {len(records)} unique wine record(s)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse rebwein.ch listing pages into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./rebwein/cache/page_*.html")
    src.add_argument("--input", type=Path, metavar="FILE", help="Read one HTML file instead of the cache")
    parser.add_argument("--output", type=Path, metavar="FILE", help="Write output JSON here (default: stdout)")
    args = parser.parse_args()

    pages = [args.input] if args.input else load_files()
    wines = parse_all(pages)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
