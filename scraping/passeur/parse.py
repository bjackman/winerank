#!/usr/bin/env python3
"""
passeur/parse.py — parse cached WooCommerce Store API pages into wine records.

Le Passeur de Vin's WooCommerce products carry no product attributes/brands, but
prices come back as integer minor units (rappen) with a CHF currency, and the
product categories give the wine type (Vin Rouge/Blanc/Rosé/Champagne/Saké/…).
Vintage and producer are recovered from the product name where present.

Usage
-----
    nix develop -c python scraping/passeur/parse.py --from-cache --output scraping/passeur/wines.json
    nix develop -c python scraping/passeur/fetch.py | nix develop -c python scraping/passeur/parse.py
"""

import argparse
import html
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "passeur" / "cache"
MERCHANT = "passeur"

# Category names that denote a wine type (lower-cased match).
_TYPE_CATEGORIES = {
    "vin rouge": "Rouge", "vin blanc": "Blanc", "vin rosé": "Rosé",
    "champagne": "Champagne", "vin effervescent": "Effervescent",
    "saké": "Saké", "shochu": "Shochu",
}


def load_from_cache() -> list[dict]:
    pages = sorted(CACHE_DIR.glob("page_*.json"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    all_products: list[dict] = []
    for p in pages:
        all_products.extend(json.loads(p.read_text(encoding="utf-8")))
    print(f"Loaded {len(all_products)} products from {len(pages)} cache files", file=sys.stderr)
    return all_products


def strip_html(text: str) -> str:
    return html.unescape(re.sub(r"<[^>]+>", "", text or "")).strip()


def parse_vintage(name: str) -> int | None:
    m = re.search(r"\b(19|20)\d{2}\b", name or "")
    return int(m.group(0)) if m else None


def parse_producer(name: str) -> str | None:
    """Le Passeur names are usually 'Cuvée … – detail, Producer' — take the
    segment after the last comma as a best-effort producer."""
    if "," in name:
        tail = name.rsplit(",", 1)[-1].strip()
        if 2 < len(tail) <= 60:
            return tail
    return None


def wine_type(categories: list[dict]) -> str | None:
    for c in categories:
        key = (c.get("name") or "").lower()
        if key in _TYPE_CATEGORIES:
            return _TYPE_CATEGORIES[key]
    return None


def parse_product(raw: dict) -> dict:
    name = strip_html(raw.get("name", ""))
    prices = raw.get("prices", {}) or {}
    price_raw = prices.get("price")
    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": parse_producer(name),
        "vintage": parse_vintage(name),
        "wine_type": wine_type(raw.get("categories", []) or []),
        "categories": [c.get("name") for c in (raw.get("categories", []) or []) if c.get("name")],
        "currency": prices.get("currency_code", "CHF"),
        "price": int(price_raw) if price_raw not in (None, "") else None,  # rappen
        "in_stock": raw.get("is_in_stock"),
        "sku": raw.get("sku") or None,
        "url": raw.get("permalink"),
    }


def parse_all(raw_products: list[dict]) -> list[dict]:
    wines = [parse_product(p) for p in raw_products]
    print(f"Parsed {len(wines)} wine records", file=sys.stderr)
    return wines


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse lepasseurdevin.ch raw API JSON into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./passeur/cache/page_*.json")
    src.add_argument("--input", type=Path, metavar="FILE", help="Read raw JSON from this file instead of stdin")
    parser.add_argument("--output", type=Path, metavar="FILE", help="Write output JSON here (default: stdout)")
    args = parser.parse_args()

    if args.from_cache:
        raw = load_from_cache()
    elif args.input:
        raw = json.loads(args.input.read_text(encoding="utf-8"))
    else:
        print("Reading raw JSON from stdin…", file=sys.stderr)
        raw = json.load(sys.stdin)

    wines = parse_all(raw)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
