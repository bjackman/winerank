#!/usr/bin/env python3
"""
moevenpick/parse.py — parse cached Magento PDPs into clean wine records.

Each product page embeds a JSON-LD ``Product`` block with one ``Offer`` per
bottle format (each with its own sku/price/availability). We emit one record per
product, using the cheapest in-stock offer as the headline price (falling back
to the cheapest offer overall), and expose the per-format offers under
``variants``. Pages with no ``Product`` block (category/CMS pages that share the
.html suffix and live in the same sitemap) are skipped.

Mövenpick's JSON-LD has no structured producer/brand, so ``producer`` is left
null; ``vintage`` is taken from the leading year in the product name.

Usage
-----
    nix develop -c python scraping/moevenpick/parse.py --from-cache
    nix develop -c python scraping/moevenpick/parse.py --from-cache --output scraping/moevenpick/wines.json
"""

import argparse
import html as html_module
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "moevenpick" / "cache"
MERCHANT = "moevenpick"
_LD_RE = re.compile(r'<script[^>]+application/ld\+json[^>]*>(.*?)</script>', re.S)


def load_from_cache() -> list[str]:
    pages = sorted(CACHE_DIR.glob("*.html"))
    if not pages:
        print("No cache files found. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    return [p.read_text(encoding="utf-8") for p in pages]


def find_product_ld(html: str) -> dict | None:
    for raw in _LD_RE.findall(html):
        try:
            obj = json.loads(raw)
        except json.JSONDecodeError:
            continue
        for item in (obj if isinstance(obj, list) else [obj]):
            if isinstance(item, dict) and item.get("@type") == "Product":
                return item
    return None


def parse_vintage(name: str) -> int | None:
    m = re.search(r"\b(19|20)\d{2}\b", name or "")
    return int(m.group(0)) if m else None


def to_rappen(price) -> int | None:
    try:
        return round(float(price) * 100)
    except (TypeError, ValueError):
        return None


def offer_list(product: dict) -> list[dict]:
    offers = product.get("offers") or []
    offers = offers if isinstance(offers, list) else [offers]
    return [o for o in offers if isinstance(o, dict)]


def pick_headline(offers: list[dict]) -> dict | None:
    """Cheapest in-stock offer, else cheapest offer overall."""
    priced = [(to_rappen(o.get("price")), o) for o in offers if to_rappen(o.get("price")) is not None]
    if not priced:
        return None
    in_stock = [(p, o) for p, o in priced if str(o.get("availability", "")).endswith("InStock")]
    pool = in_stock or priced
    return min(pool, key=lambda po: po[0])[1]


def product_to_record(product: dict) -> dict:
    name = html_module.unescape(product.get("name") or "").strip()
    offers = offer_list(product)
    headline = pick_headline(offers)
    return {
        "merchant": MERCHANT,
        "name": name,
        "producer": None,  # not exposed in JSON-LD; would need PDP body heuristics
        "vintage": parse_vintage(name),
        "currency": (headline or {}).get("priceCurrency", "CHF"),
        "price": to_rappen((headline or {}).get("price")),
        "in_stock": any(str(o.get("availability", "")).endswith("InStock") for o in offers),
        "sku": product.get("sku") or (headline or {}).get("sku"),
        "url": product.get("url"),
        "variants": [
            {
                "sku": o.get("sku"),
                "price": to_rappen(o.get("price")),
                "in_stock": str(o.get("availability", "")).endswith("InStock"),
            }
            for o in offers
        ],
    }


def parse_all(pages: list[str]) -> list[dict]:
    records: list[dict] = []
    skipped = 0
    for html in pages:
        product = find_product_ld(html)
        if product is None:
            skipped += 1
            continue
        records.append(product_to_record(product))
    print(f"Parsed {len(records)} wine records ({skipped} non-product pages skipped)", file=sys.stderr)
    return records


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse moevenpick-wein.com PDPs into wine records")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./moevenpick/cache/*.html")
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
