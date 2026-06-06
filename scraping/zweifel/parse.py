#!/usr/bin/env python3
"""
zweifel/parse.py — parse cached PDPs into clean wine records.

Zweifel PDPs expose the product as schema.org microdata in static HTML:
``<meta itemprop="price" content="37.0">``, ``itemprop="currency"``,
``itemprop="availability" content=" in_stock ">``, plus the name in ``<h1>`` and
the bottle size in ``<span class="product-size">``. The URL path carries the
facets: ``/de/p/<type>/<country>/<region>/<producer>/<name>-<number>.html``.

Usage
-----
    nix develop -c python scraping/zweifel/parse.py --from-cache
    nix develop -c python scraping/zweifel/parse.py --from-cache --output scraping/zweifel/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path
from urllib.parse import urlparse

CACHE_DIR = Path.cwd() / "scraping" / "zweifel" / "cache"
MERCHANT = "zweifel"

_H1_RE = re.compile(r"<h1[^>]*>(.*?)</h1>", re.S)
_PRICE_RE = re.compile(r'itemprop="price"\s+content="([^"]+)"')
_CURRENCY_RE = re.compile(r'itemprop="currency"\s+content="([^"]+)"')
_AVAIL_RE = re.compile(r'itemprop="availability"\s+content="([^"]+)"')
_SIZE_RE = re.compile(r'class="product-size"[^>]*>(.*?)</span>', re.S)
# Vintage sits in its own subtitle element (a sibling of the producer subtitle),
# e.g. <p class="product-subtitle">2021</p>; the name/slug usually omits it.
_VINTAGE_SUBTITLE_RE = re.compile(r'class="product-subtitle"[^>]*>\s*((?:19|20)\d{2})\s*<')
_CANON_RE = re.compile(r'<link[^>]+rel="canonical"[^>]+href="([^"]+)"')
_KEYS_RE = re.compile(r'data-shop-productkeys="([^"]*)"')


def load_files() -> list[Path]:
    pages = sorted(CACHE_DIR.glob("*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return pages


def strip_tags(text: str) -> str:
    return html_module.unescape(re.sub(r"<[^>]+>", "", text or "")).strip()


def to_rappen(price) -> int | None:
    try:
        return round(float(price) * 100)
    except (TypeError, ValueError):
        return None


def parse_vintage(*texts: str) -> int | None:
    for t in texts:
        m = re.search(r"\b(19|20)\d{2}\b", t or "")
        if m:
            return int(m.group(0))
    return None


def parse_bottle_ml(size_text: str | None) -> int | None:
    if not size_text:
        return None
    m = re.search(r"([\d.]+)\s*(cl|ml|l)\b", size_text, re.I)
    if not m:
        return None
    return round(float(m.group(1)) * {"cl": 10, "ml": 1, "l": 1000}[m.group(2).lower()])


def url_facets(url: str) -> dict:
    """type/country/region/producer from /de/p/<type>/<country>/<region>/<producer>/<slug>."""
    parts = [p for p in urlparse(url).path.split("/") if p]
    facets = {"wine_type": None, "country": None, "region": None, "producer": None}
    if len(parts) >= 3 and parts[1] == "p":
        body = parts[2:-1]  # drop trailing product slug
        keys = ["wine_type", "country", "region", "producer"]
        for key, seg in zip(keys, body):
            facets[key] = seg.replace("-", " ")
        # if there are more than 4 facet segments, the producer is the last one
        if len(body) > 4:
            facets["producer"] = body[-1].replace("-", " ")
    return facets


def page_to_record(html: str) -> dict | None:
    m_h1 = _H1_RE.search(html)
    if not m_h1:
        return None
    name = strip_tags(m_h1.group(1))
    if not name:
        return None

    m_url = _CANON_RE.search(html)
    url = html_module.unescape(m_url.group(1)) if m_url else None
    facets = url_facets(url or "")

    m_avail = _AVAIL_RE.search(html)
    in_stock = ("in_stock" in m_avail.group(1)) if m_avail else None

    m_price = _PRICE_RE.search(html)
    m_cur = _CURRENCY_RE.search(html)
    m_size = _SIZE_RE.search(html)
    m_keys = _KEYS_RE.search(html)

    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": facets["producer"],
        "vintage": (
            int(m_v.group(1)) if (m_v := _VINTAGE_SUBTITLE_RE.search(html))
            else parse_vintage(name, url or "")
        ),
        "country": facets["country"],
        "region": facets["region"],
        "wine_type": facets["wine_type"],
        "currency": (m_cur.group(1) if m_cur else "CHF"),
        "price": to_rappen(m_price.group(1)) if m_price else None,
        "in_stock": in_stock,
        "sku": (m_keys.group(1).split("_")[0] if m_keys else None),
        "bottle_size_ml": parse_bottle_ml(strip_tags(m_size.group(1)) if m_size else None),
        "url": url,
    }


def parse_all(pages: list[Path]) -> list[dict]:
    records, skipped = [], 0
    for p in pages:
        rec = page_to_record(p.read_text(encoding="utf-8"))
        if rec is None:
            skipped += 1
            continue
        records.append(rec)
    print(f"Parsed {len(records)} wine records ({skipped} pages skipped)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse zweifel1898.ch PDPs into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./zweifel/cache/*.html")
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
