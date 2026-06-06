#!/usr/bin/env python3
"""
smith-and-smith/parse.py — parse cached /de/shop HTML into clean wine records.

Smith & Smith's listing has no JSON-LD product data, so we parse the
``card artikel-box`` product cards directly. Each card is richly structured:

  - ``data-artikelid``                 → sku (internal article id)
  - ``<a href="/de/shop/<cat>/<slug>">`` → product URL
  - ``artikel-box__herkunft`` (2 spans) → producer + "Region, Country"
  - ``artikel-box__name``               → name
  - ``artikel-box__eigenschaften`` spans → vintage + bottle size
  - ``artikel-box__preisaktuell``       → price ("CHF <!-- -->43.00")

Usage
-----
    nix develop -c python scraping/smith-and-smith/parse.py --from-cache
    nix develop -c python scraping/smith-and-smith/parse.py --from-cache --output scraping/smith-and-smith/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "smith-and-smith" / "cache"
MERCHANT = "smith-and-smith"
BASE = "https://www.smithandsmith.ch"

_CARD_SPLIT = 'class="card artikel-box"'
_ARTIKELID_RE = re.compile(r'data-artikelid="(\d+)"')
_URL_RE = re.compile(r'href="(/de/shop/[^"]+)"')
_HERKUNFT_RE = re.compile(r'artikel-box__herkunft[^>]*>(.*?)</div>', re.S)
_NAME_RE = re.compile(r'artikel-box__name[^>]*>(.*?)</div>', re.S)
_EIGEN_RE = re.compile(r'artikel-box__eigenschaften[^>]*>(.*?)</div>', re.S)
_PRICE_RE = re.compile(r'artikel-box__preisaktuell[^>]*>(.*?)</span>', re.S)
_SPAN_RE = re.compile(r'<span[^>]*>(.*?)</span>', re.S)


def load_from_cache() -> list[str]:
    pages = sorted(CACHE_DIR.glob("page_*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return [p.read_text(encoding="utf-8") for p in pages]


def strip_tags(text: str) -> str:
    text = re.sub(r"<!--.*?-->", "", text or "", flags=re.S)
    text = re.sub(r"<[^>]+>", "", text)
    return html_module.unescape(text).strip()


def spans(block: str) -> list[str]:
    return [strip_tags(s) for s in _SPAN_RE.findall(block) if strip_tags(s)]


def parse_price(raw: str) -> int | None:
    """'CHF <!-- -->43.00' → 4300 (rappen)."""
    digits = re.sub(r"[^0-9.]", "", strip_tags(raw))
    if not digits or digits == ".":
        return None
    try:
        return round(float(digits) * 100)
    except ValueError:
        return None


def parse_vintage(values: list[str]) -> int | None:
    for v in values:
        m = re.fullmatch(r"(19|20)\d{2}", v.strip())
        if m:
            return int(v)
    return None


def parse_bottle_ml(values: list[str]) -> int | None:
    for v in values:
        m = re.search(r"([\d.]+)\s*(cl|ml|l)\b", v, re.I)
        if m:
            num, unit = float(m.group(1)), m.group(2).lower()
            return round(num * {"cl": 10, "ml": 1, "l": 1000}[unit])
    return None


def card_to_record(card: str) -> dict | None:
    m_url = _URL_RE.search(card)
    m_name = _NAME_RE.search(card)
    if not m_url or not m_name:
        return None
    url = BASE + html_module.unescape(m_url.group(1))
    name = strip_tags(m_name.group(1))
    if not name:
        return None

    m_id = _ARTIKELID_RE.search(card)
    sku = m_id.group(1) if m_id else None

    producer = country = region = None
    m_herk = _HERKUNFT_RE.search(card)
    if m_herk:
        parts = spans(m_herk.group(1))
        if parts:
            producer = parts[0]
        if len(parts) > 1:
            # second span is "Region, Country" (e.g. "Aargau, Schweiz")
            loc = [p.strip() for p in parts[1].split(",")]
            region = loc[0] or None
            country = loc[-1] if len(loc) > 1 else None

    eigen = spans(_EIGEN_RE.search(card).group(1)) if _EIGEN_RE.search(card) else []

    m_price = _PRICE_RE.search(card)
    price = parse_price(m_price.group(1)) if m_price else None

    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": producer,
        "vintage": parse_vintage(eigen),
        "country": country,
        "region": region,
        "currency": "CHF",
        "price": price,
        "in_stock": price is not None,
        "sku": sku,
        "bottle_size_ml": parse_bottle_ml(eigen),
        "url": url,
    }


def parse_all(pages: list[str]) -> list[dict]:
    records: list[dict] = []
    seen: set[str] = set()
    for i, html in enumerate(pages, 1):
        cards = html.split(_CARD_SPLIT)[1:]
        page_records = [r for c in cards if (r := card_to_record(c))]
        print(f"  [page {i:03d}] {len(page_records)} product(s) extracted", file=sys.stderr)
        for r in page_records:
            if r["url"] in seen:
                continue
            seen.add(r["url"])
            records.append(r)
    print(f"Total: {len(records)} unique wine record(s)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse smithandsmith.ch /de/shop HTML into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./smith-and-smith/cache/page_*.html")
    src.add_argument("--input", type=Path, metavar="FILE", help="Read one HTML file instead of the cache")
    parser.add_argument("--output", type=Path, metavar="FILE", help="Write output JSON here (default: stdout)")
    args = parser.parse_args()

    if args.input:
        pages = [args.input.read_text(encoding="utf-8")]
    else:
        pages = load_from_cache()

    wines = parse_all(pages)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
