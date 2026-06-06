#!/usr/bin/env python3
"""
gerstl/parse.py — parse cached Angular PDPs into clean wine records.

Each PDP embeds an Angular TransferState blob
(`<script id="ng-state" type="application/json">`) that contains a fully
structured product object: nested `{key,name}` dicts for country/region/
appellation/wineType/year/vendor, a numeric `price`, `grapes`, `size`,
`stock`, `sku`. We pull the blob, find the product whose `slug` matches the
cache file (the PDP blob may also carry related products), and flatten it.

Usage
-----
    nix develop -c python scraping/gerstl/parse.py --from-cache
    nix develop -c python scraping/gerstl/parse.py --from-cache --output scraping/gerstl/wines.json
"""

import argparse
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "gerstl" / "cache"
MERCHANT = "gerstl"
BASE = "https://www.gerstl.ch"
_NGSTATE_RE = re.compile(r'<script[^>]*id="ng-state"[^>]*>(.*?)</script>', re.S)


def load_files() -> list[Path]:
    pages = sorted(p for p in CACHE_DIR.glob("*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return pages


def iter_products(node):
    """Yield every dict that looks like a Gerstl product object."""
    if isinstance(node, dict):
        if {"sku", "year", "price", "slug"} <= node.keys():
            yield node
        for v in node.values():
            yield from iter_products(v)
    elif isinstance(node, list):
        for v in node:
            yield from iter_products(v)


def to_rappen(price) -> int | None:
    try:
        return round(float(price) * 100)
    except (TypeError, ValueError):
        return None


def name_of(d) -> str | None:
    """A nested {key,name} dict → its name; a plain string → itself."""
    if isinstance(d, dict):
        return d.get("name")
    return d or None


def product_to_record(p: dict) -> dict:
    title1 = (p.get("title1") or "").strip()
    title3 = (p.get("title3") or "").strip()
    name = title3 or title1
    if title1 and title3 and title1 not in title3:
        name = f"{title1} {title3}"

    size = p.get("size") or {}
    bottle_ml = round(size["value"] * 10) if isinstance(size.get("value"), (int, float)) else None
    stock = p.get("stock") or {}
    grapes = p.get("grapes") if isinstance(p.get("grapes"), list) else []
    year = name_of(p.get("year"))

    return {
        "merchant": MERCHANT,
        "name": name or None,
        "producer": name_of(p.get("vendor")),
        "vintage": int(year) if year and str(year).isdigit() else None,
        "country": name_of(p.get("country")),
        "region": name_of(p.get("region")),
        "appellation": name_of(p.get("appellation")),
        "wine_type": name_of(p.get("wineType")) or name_of(p.get("color")),
        "grapes": grapes,
        "currency": "CHF",
        "price": to_rappen(p.get("price")),
        "in_stock": (stock.get("stock") or 0) > 0 if "stock" in stock else None,
        "sku": p.get("sku"),
        "bottle_size_ml": bottle_ml,
        "url": f"{BASE}/{p['slug']}/p" if p.get("slug") else None,
    }


def page_to_record(html: str, want_slug: str) -> dict | None:
    m = _NGSTATE_RE.search(html)
    if not m:
        return None
    try:
        blob = json.loads(m.group(1))
    except json.JSONDecodeError:
        return None
    products = list(iter_products(blob))
    if not products:
        return None
    # Prefer the product matching this page's slug; else the first one.
    chosen = next((p for p in products if p.get("slug") == want_slug), products[0])
    return product_to_record(chosen)


def parse_all(pages: list[Path]) -> list[dict]:
    records, skipped = [], 0
    for p in pages:
        rec = page_to_record(p.read_text(encoding="utf-8"), p.stem)
        if rec is None:
            skipped += 1
            continue
        records.append(rec)
    print(f"Parsed {len(records)} wine records ({skipped} pages skipped)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse gerstl.ch PDPs into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./gerstl/cache/*.html")
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
