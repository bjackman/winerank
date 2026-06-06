#!/usr/bin/env python3
"""
arvi/parse.py — parse cached nopCommerce category HTML into clean wine records.

arvi.ch (nopCommerce) exposes no JSON-LD or structured product data on its
listing pages, so we parse the ``.item-box`` product cards directly. Each card
carries everything we need without a PDP fetch:

  - ``data-productid``            → sku (nopCommerce product id)
  - ``<h2 class="product-title">`` → name (+ the product URL)
  - ``<div class="manufacturer-title">`` → producer (clean, structured)
  - ``<div class="manufacturer-size">``  → bottle size
  - ``<span class="price actual-price">`` → price (the *visible* one; each card
    also holds hidden alternate-format prices in ``display-none`` divs)

The product URL is itself structured —
``/en/<producer>/<wine>/<vintage>/<bottle-size>`` — so vintage and bottle size
are recovered from the slug as a fallback (vintage may be ``nv``).

Usage
-----
    nix develop -c python scraping/arvi/parse.py --from-cache
    nix develop -c python scraping/arvi/parse.py --from-cache --output scraping/arvi/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "arvi" / "cache"
MERCHANT = "arvi"
BASE = "https://arvi.ch"

# A product card is everything from one item-box opener to the next.
_CARD_SPLIT = '<div class="item-box">'
_PRODUCTID_RE = re.compile(r'data-productid="(\d+)"')
_TITLE_RE = re.compile(
    r'<h2 class="product-title">\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>', re.S
)
_PRODUCER_RE = re.compile(r'<div class="manufacturer-title">(.*?)</div>', re.S)
_SIZE_RE = re.compile(r'<div class="manufacturer-size">(.*?)</div>', re.S)
# Price divs: the visible one has no class; alternate formats are display-none.
_PRICE_DIV_RE = re.compile(
    r'<div id="owc_price_[^"]*"(?P<cls>[^>]*)>\s*'
    r'<span class="price actual-price">\s*([^<]+?)\s*</span>',
    re.S,
)


def load_from_cache() -> list[str]:
    pages = sorted(CACHE_DIR.glob("page_*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return [p.read_text(encoding="utf-8") for p in pages]


def strip_tags(text: str) -> str:
    text = re.sub(r"<[^>]+>", "", text or "")
    return html_module.unescape(text).strip()


def parse_price(raw: str) -> int | None:
    """'CHF 1’524.30' → 152430 (rappen). Drops the thousands separator."""
    digits = re.sub(r"[^0-9.]", "", html_module.unescape(raw))
    if not digits or digits == ".":
        return None
    try:
        return round(float(digits) * 100)
    except ValueError:
        return None


def parse_bottle_ml(size_text: str | None, url: str) -> int | None:
    """Bottle size in ml, from the visible size label or the URL slug."""
    for src in (size_text or "", url):
        # "37.5cl" / "37_5-cl" / "75cl" / "150 cl"
        m = re.search(r"(\d+(?:[._]\d+)?)\s*-?\s*cl", src, re.I)
        if m:
            return round(float(m.group(1).replace("_", ".")) * 10)
    return None


def parse_vintage(url: str, name: str) -> int | None:
    """Vintage from the URL slug (…/<vintage>/<bottle>), falling back to name."""
    segs = [s for s in url.split("/") if s]
    if len(segs) >= 2:
        m = re.fullmatch(r"(19|20)\d{2}", segs[-2])
        if m:
            return int(segs[-2])
    m = re.search(r"\b(19|20)\d{2}\b", name)
    return int(m.group(0)) if m else None


def card_to_record(card: str) -> dict | None:
    m_title = _TITLE_RE.search(card)
    if not m_title:
        return None
    url = html_module.unescape(m_title.group(1))
    if url.startswith("/"):
        url = BASE + url
    name = strip_tags(m_title.group(2))

    m_pid = _PRODUCTID_RE.search(card)
    sku = m_pid.group(1) if m_pid else None

    m_prod = _PRODUCER_RE.search(card)
    producer = strip_tags(m_prod.group(1)) if m_prod else None

    m_size = _SIZE_RE.search(card)
    size_text = strip_tags(m_size.group(1)) if m_size else None

    # Prefer the visible price (no class attr); else first price found.
    price = None
    for m in _PRICE_DIV_RE.finditer(card):
        candidate = parse_price(m.group(2))
        if candidate is None:
            continue
        if "display-none" not in m.group("cls"):
            price = candidate
            break
        if price is None:
            price = candidate

    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": producer or None,
        "vintage": parse_vintage(url, name),
        "currency": "CHF",
        "price": price,
        "in_stock": price is not None,  # listing only shows purchasable cards
        "sku": sku,
        "bottle_size_ml": parse_bottle_ml(size_text, url),
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
    parser = argparse.ArgumentParser(description="Parse arvi.ch category HTML into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./arvi/cache/page_*.html")
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
