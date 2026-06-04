#!/usr/bin/env python3
"""
vinazion/parse.py — parse cached WooCommerce Store API pages into wine records.

Same shape as realwines/parse.py, but Vinazion models the interesting fields as
German product attributes (taxonomies): Jahrgang (vintage), Weingut (producer),
Land (country), Region, rebsorten (grapes), Volumen % (alcohol), Flascheninhalt
(bottle size). Prices come back as integer minor units (rappen) with a CHF
currency, exactly like realwines.

Usage
-----
    nix develop -c python vinazion/parse.py --from-cache --output vinazion/wines.json
    nix develop -c python vinazion/fetch.py | nix develop -c python vinazion/parse.py
"""

import argparse
import html
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "vinazion" / "cache"

# Map our record field → the WooCommerce attribute name Vinazion uses.
ATTR_PRODUCER = "Weingut"
ATTR_VINTAGE = "Jahrgang"
ATTR_COUNTRY = "Land"
ATTR_REGION = "Region"
ATTR_GRAPES = "rebsorten"
ATTR_ALCOHOL = "Volumen %"
ATTR_BOTTLE = "Flascheninhalt"


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
    text = re.sub(r"<[^>]+>", "", text or "")
    return html.unescape(text).strip()


def attr_terms(attributes: list[dict], name: str) -> list[str]:
    """All term names for a named attribute (case-insensitive)."""
    for attr in attributes:
        if (attr.get("name") or "").lower() == name.lower():
            return [t.get("name") for t in attr.get("terms", []) if t.get("name")]
    return []


def attr_first(attributes: list[dict], name: str) -> str | None:
    terms = attr_terms(attributes, name)
    return terms[0] if terms else None


def parse_vintage(value: str | None) -> int | None:
    if not value:
        return None
    m = re.search(r"\b(19|20)\d{2}\b", value)
    return int(m.group(0)) if m else None


def parse_product(raw: dict) -> dict:
    prices = raw.get("prices", {}) or {}
    attributes = raw.get("attributes", []) or []

    price_raw = prices.get("price")
    price_int = int(price_raw) if price_raw not in (None, "") else None

    return {
        "merchant": "vinazion",
        "name": strip_html(raw.get("name", "")),
        "producer": attr_first(attributes, ATTR_PRODUCER),
        "vintage": parse_vintage(attr_first(attributes, ATTR_VINTAGE)),
        "country": attr_first(attributes, ATTR_COUNTRY),
        "region": attr_first(attributes, ATTR_REGION),
        "grapes": attr_terms(attributes, ATTR_GRAPES),
        "currency": prices.get("currency_code", "CHF"),
        "price": price_int,  # minor units (rappen)
        "in_stock": raw.get("is_in_stock"),
        "sku": raw.get("sku") or None,
        "url": raw.get("permalink"),
    }


def parse_all(raw_products: list[dict]) -> list[dict]:
    wines = [parse_product(p) for p in raw_products]
    print(f"Parsed {len(wines)} wine records", file=sys.stderr)
    return wines


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse vinazion.ch raw API JSON into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./vinazion/cache/page_*.json")
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
