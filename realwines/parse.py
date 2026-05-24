#!/usr/bin/env python3
"""
realwines/parse.py — parse cached API pages into clean wine records.

Reads raw API JSON from stdin (or --input file) and writes a clean JSON array
to stdout (or --output file).

Usage
-----
    # Parse from cached pages directly (no network):
    python3 realwines/parse.py --from-cache

    # Or pipe from fetcher:
    python3 realwines/fetch.py | python3 realwines/parse.py

    # Write output to file:
    python3 realwines/parse.py --from-cache --output realwines/wines.json
"""

import argparse
import html
import json
import re
import sys
from pathlib import Path

# Resolved from CWD so it works both in-tree and when baked into the Nix store.
CACHE_DIR = Path.cwd() / "realwines" / "cache"


def load_from_cache() -> list[dict]:
    """Load all cached page files and concatenate their contents."""
    pages = sorted(CACHE_DIR.glob("page_*.json"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    all_products = []
    for p in pages:
        all_products.extend(json.loads(p.read_text(encoding="utf-8")))
    print(f"Loaded {len(all_products)} products from {len(pages)} cache files", file=sys.stderr)
    return all_products


def strip_html(text: str) -> str:
    """Remove HTML tags and decode HTML entities."""
    text = re.sub(r"<[^>]+>", "", text)
    return html.unescape(text).strip()


def parse_price_chf(price_str: str | None) -> float | None:
    """Convert WooCommerce price string (integer cents) to CHF float."""
    if not price_str:
        return None
    try:
        # WooCommerce returns prices as integer strings in the minor unit (Rappen)
        return round(int(price_str) / 100, 2)
    except (ValueError, TypeError):
        return None


def get_attribute(attributes: list[dict], name: str) -> str | None:
    """Extract a named attribute's first term value."""
    for attr in attributes:
        if attr.get("name", "").lower() == name.lower():
            terms = attr.get("terms", [])
            if terms:
                return terms[0].get("name")
    return None


def get_attribute_all(attributes: list[dict], name: str) -> list[str]:
    """Extract all term values for a named attribute (for multi-value fields)."""
    for attr in attributes:
        if attr.get("name", "").lower() == name.lower():
            return [t["name"] for t in attr.get("terms", [])]
    return []


def parse_product(raw: dict) -> dict:
    """Map a raw WooCommerce Store API product to a clean wine record."""
    prices = raw.get("prices", {}) or {}
    images = raw.get("images", []) or []
    attributes = raw.get("attributes", []) or []
    stock_info = raw.get("stock_availability", {}) or {}

    return {
        # Identity
        "id": raw.get("id"),
        "sku": raw.get("sku"),
        "name": strip_html(raw.get("name", "")),
        "slug": raw.get("slug"),
        "url": raw.get("permalink"),
        # Pricing (CHF)
        "currency": prices.get("currency_code", "CHF"),
        "price_chf": parse_price_chf(prices.get("price")),
        "regular_price_chf": parse_price_chf(prices.get("regular_price")),
        "sale_price_chf": parse_price_chf(prices.get("sale_price")),
        "on_sale": raw.get("on_sale", False),
        # Availability
        "in_stock": raw.get("is_in_stock", False),
        "on_backorder": raw.get("is_on_backorder", False),
        "low_stock_remaining": raw.get("low_stock_remaining"),
        "stock_status": stock_info.get("text"),
        # Description
        "short_description": strip_html(raw.get("short_description", "")),
        # Attributes (wine-specific fields from WooCommerce product attributes)
        "size": get_attribute(attributes, "size"),
        "vintage": get_attribute(attributes, "vintage"),
        "region": get_attribute(attributes, "region"),
        "appellation": get_attribute(attributes, "appellation"),
        "grape": get_attribute(attributes, "grapes"),  # API uses plural
        "producer": get_attribute(attributes, "producer"),
        "country": get_attribute(attributes, "country"),
        "colour": get_attribute(attributes, "colour"),
        "style": get_attribute(attributes, "style"),
        "star_rating": get_attribute(attributes, "star-rating"),
        "when_to_drink": get_attribute(attributes, "when to drink"),
        "wine_category": get_attribute(attributes, "winecategory"),
        "product_group": get_attribute(attributes, "productgroup"),
        # Multi-value attributes (list of all terms)
        "specialisms": get_attribute_all(attributes, "our specialisms"),
        "popular_tags": get_attribute_all(attributes, "popular"),
        "press_awards": get_attribute_all(attributes, "press awards"),
        # Ratings
        "average_rating": float(raw["average_rating"]) if raw.get("average_rating") else None,
        "review_count": raw.get("review_count", 0),
        # Image
        "image_url": images[0]["src"] if images else None,
        # Raw attributes list for anything we didn't map explicitly
        "_raw_attributes": [
            {"name": a["name"], "values": [t["name"] for t in a.get("terms", [])]}
            for a in attributes
        ],
    }


def parse_all(raw_products: list[dict]) -> list[dict]:
    wines = [parse_product(p) for p in raw_products]
    print(f"Parsed {len(wines)} wine records", file=sys.stderr)
    return wines


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse realwines.ch raw API JSON into clean wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument(
        "--from-cache",
        action="store_true",
        help="Load directly from ./cache/page_*.json (no stdin needed)",
    )
    src.add_argument(
        "--input",
        type=Path,
        metavar="FILE",
        help="Read raw JSON from this file instead of stdin",
    )
    parser.add_argument(
        "--output",
        type=Path,
        metavar="FILE",
        help="Write output JSON to this file (default: stdout)",
    )
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
