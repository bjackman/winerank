#!/usr/bin/env python3
"""
bauraulac/parse.py — parse cached PDPs into clean wine records.

Baur au Lac PDPs carry the product data in static HTML as `data-shop-product*`
attributes (name, sku/number, price, currency), with stock shown via a
`productdetail__stock-icon--available` class. The URL path encodes the rest:
`/de/p/<type>/<country>/<region>/…/<name>-<vintage>-<number>.html`.

Usage
-----
    nix develop -c python scraping/bauraulac/parse.py --from-cache
    nix develop -c python scraping/bauraulac/parse.py --from-cache --output scraping/bauraulac/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path
from urllib.parse import urlparse

CACHE_DIR = Path.cwd() / "scraping" / "bauraulac" / "cache"
MERCHANT = "bauraulac"

_ATTR_RE = re.compile(r'(data-shop-[a-z]+|data-product-id)="([^"]*)"')
_CANON_RE = re.compile(r'<link[^>]+rel="canonical"[^>]+href="([^"]+)"')
_OG_URL_RE = re.compile(r'<meta[^>]+property="og:url"[^>]+content="([^"]+)"')


def load_files() -> list[Path]:
    pages = sorted(p for p in CACHE_DIR.glob("*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return pages


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


def url_facets(url: str) -> dict:
    """type / country / region from the /de/p/<type>/<country>/<region>/… path."""
    parts = [p for p in urlparse(url).path.split("/") if p]
    # parts: ['de', 'p', <type>, <country>, <region?>, …, <slug>]
    facets = {"wine_type": None, "country": None, "region": None}
    if len(parts) >= 3 and parts[1] == "p":
        body = parts[2:-1]  # drop the trailing product slug
        if len(body) >= 1:
            facets["wine_type"] = body[0].replace("-", " ")
        if len(body) >= 2:
            facets["country"] = body[1].replace("-", " ")
        if len(body) >= 3:
            facets["region"] = body[2].replace("-", " ")
    return facets


def page_to_record(html: str) -> dict | None:
    attrs = {k: html_module.unescape(v) for k, v in _ATTR_RE.findall(html)}
    name = attrs.get("data-shop-productname")
    if not name:
        return None

    m_url = _CANON_RE.search(html) or _OG_URL_RE.search(html)
    url = html_module.unescape(m_url.group(1)) if m_url else None

    facets = url_facets(url or "")
    in_stock = (
        "productdetail__stock-icon--available" in html
        if "productdetail__stock-icon--" in html
        else None
    )

    return {
        "merchant": MERCHANT,
        "name": name.strip(),
        "producer": None,  # not exposed as a structured field
        "vintage": parse_vintage(name, url or ""),
        "country": facets["country"],
        "region": facets["region"],
        "wine_type": facets["wine_type"],
        "currency": attrs.get("data-shop-productcurrency", "CHF"),
        "price": to_rappen(attrs.get("data-shop-productprice")),
        "in_stock": in_stock,
        "sku": attrs.get("data-shop-productnumber") or attrs.get("data-product-id"),
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
    parser = argparse.ArgumentParser(description="Parse bauraulacvins.ch PDPs into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./bauraulac/cache/*.html")
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
