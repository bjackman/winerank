#!/usr/bin/env python3
"""
realwines/parse.py — parse cached API pages into clean wine records.

Reads raw API JSON from stdin (or --input file) and writes a clean JSON array
to stdout (or --output file).

Usage
-----
    # Parse from cached pages directly (no network):
    python3 scraping/realwines/parse.py --from-cache

    # Or pipe from fetcher:
    python3 scraping/realwines/fetch.py | python3 scraping/realwines/parse.py

    # Write output to file:
    python3 scraping/realwines/parse.py --from-cache --output scraping/realwines/wines.json
"""

import argparse
import html
import json
import re
import sys
from pathlib import Path

# Resolved from CWD so it works both in-tree and when baked into the Nix store.
CACHE_DIR = Path.cwd() / "scraping" / "realwines" / "cache"


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


def get_attribute(attributes: list[dict], name: str) -> str | None:
    """Extract a named attribute's first term value."""
    for attr in attributes:
        if attr.get("name", "").lower() == name.lower():
            terms = attr.get("terms", [])
            if terms:
                return terms[0].get("name")
    return None


def parse_product(raw: dict) -> dict:
    """Map a raw WooCommerce Store API product to a minimal wine record."""
    prices = raw.get("prices", {}) or {}
    attributes = raw.get("attributes", []) or []

    price_raw = prices.get("price")
    price_int = int(price_raw) if price_raw is not None else None

    return {
        "name": strip_html(raw.get("name", "")),
        "currency": prices.get("currency_code", "CHF"),
        "price": price_int,
        "vintage": get_attribute(attributes, "vintage"),
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
