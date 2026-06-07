#!/usr/bin/env python3
"""
flaschenpost/parse.py — parse cached HTML pages into clean wine records.

Reads raw HTML from cache/page_*.html, reconstructs the Next.js RSC payload,
brace-matches product objects, and emits a clean JSON array.

Usage
-----
    nix develop -c python scraping/flaschenpost/parse.py --from-cache
    nix develop -c python scraping/flaschenpost/parse.py --from-cache --output scraping/flaschenpost/wines.json
"""

import argparse
import json
import re
import sys
from pathlib import Path

CACHE_DIR = Path.cwd() / "scraping" / "flaschenpost" / "cache"
MERCHANT = "flaschenpost"
LOCALE = "de-CH"


# ---------------------------------------------------------------------------
# RSC reconstruction
# ---------------------------------------------------------------------------

def reconstruct_rsc_payload(html: str) -> str:
    """Reconstruct the Next.js RSC stream payload from a page's HTML."""
    chunks = re.findall(r'self\.__next_f\.push\(\[1,(".*?")\]\)', html, re.S)
    return "".join(json.loads(c) for c in chunks)


# ---------------------------------------------------------------------------
# Product object extraction (brace-matching)
# ---------------------------------------------------------------------------

def extract_products(payload: str) -> list[dict]:
    """
    Brace-match every "product":{...} object in the RSC payload and return
    those that look like real product records (contain objectID or sku).
    """
    results = []
    search = 0
    while True:
        start = payload.find('"product":{', search)
        if start == -1:
            break
        obj_start = start + len('"product":')
        depth, pos = 0, obj_start
        while pos < len(payload):
            if payload[pos] == '{':
                depth += 1
            elif payload[pos] == '}':
                depth -= 1
                if depth == 0:
                    try:
                        obj = json.loads(payload[obj_start : pos + 1])
                        if 'objectID' in obj or 'sku' in obj:
                            results.append(obj)
                    except Exception:
                        pass
                    search = pos + 1
                    break
            pos += 1
        else:
            break
    return results


# ---------------------------------------------------------------------------
# Field helpers
# ---------------------------------------------------------------------------

def _locale_str(value, locale: str = LOCALE) -> str | None:
    """Extract a string from a multilingual dict, falling back to any value."""
    if isinstance(value, str):
        return value or None
    if isinstance(value, dict):
        if locale in value:
            return value[locale] or None
        # Try common fallbacks
        for key in ("de", "en", "fr-CH", "it-CH"):
            if key in value and value[key]:
                return value[key]
        # Return first non-empty value
        for v in value.values():
            if isinstance(v, str) and v:
                return v
    return None


def _parse_vintage(attrs: dict) -> int | None:
    """Extract vintage year as int, or None."""
    for key in ("vintage", "year"):
        val = attrs.get(key)
        if val is not None:
            try:
                year = int(val)
                if 1800 <= year <= 2100:
                    return year
            except (ValueError, TypeError):
                pass
    return None


def _parse_price(product: dict) -> int | None:
    """
    Return the effective price as integer CHF cents (already integer in API).
    Prefer discountPrice.amount when isActive, else initialPrice.amount.
    """
    price_block = product.get("price", {})
    if not isinstance(price_block, dict):
        return None

    discount = price_block.get("discountPrice", {}) or {}
    initial = price_block.get("initialPrice", {}) or {}

    if discount.get("isActive") and discount.get("amount") is not None:
        try:
            return int(discount["amount"])
        except (ValueError, TypeError):
            pass

    if initial.get("amount") is not None:
        try:
            return int(initial["amount"])
        except (ValueError, TypeError):
            pass

    return None


# ---------------------------------------------------------------------------
# Record mapping
# ---------------------------------------------------------------------------

def parse_product(product: dict) -> dict | None:
    """
    Map a raw Flaschenpost product object to a clean wine record.
    Returns None if key fields are missing or parsing fails.
    """
    try:
        attrs = product.get("attributes", {}) or {}

        # --- Required fields ---
        sku = product.get("sku")
        if not sku:
            return None

        # name: prefer attributes.displayName (plain string), fall back to name dict
        name = attrs.get("displayName") or _locale_str(product.get("name"))
        if not name:
            return None

        # producer
        producer_field = attrs.get("producer", {}) or {}
        if isinstance(producer_field, dict):
            producer = producer_field.get("label")
        else:
            producer = str(producer_field) if producer_field else None
        if not producer:
            return None

        price = _parse_price(product)
        if price is None:
            return None

        # stock
        stock = product.get("stock", {}) or {}
        in_stock = bool(stock.get("isAvailable", False))

        # url
        product_url_path = product.get("url", "")
        url = f"https://www.flaschenpost.ch/{product_url_path}" if product_url_path else None
        if not url:
            return None

        record: dict = {
            "merchant": MERCHANT,
            "name": name,
            "producer": producer,
            "vintage": _parse_vintage(attrs),
            "currency": "CHF",
            "price": price,
            "in_stock": in_stock,
            "sku": str(sku),
            "url": url,
        }

        # --- Optional extras ---
        country = _locale_str(product.get("country"))
        if country:
            record["country"] = country

        wine_type_field = attrs.get("wineType", {}) or {}
        if isinstance(wine_type_field, dict):
            wine_type_label = wine_type_field.get("label")
            wine_type = _locale_str(wine_type_label)
        else:
            wine_type = None
        if wine_type:
            record["wine_type"] = wine_type

        alcohol = attrs.get("alcohol")
        if alcohol is not None:
            try:
                record["alcohol"] = float(alcohol)
            except (ValueError, TypeError):
                pass

        bottle_size = attrs.get("bottleSize")
        if bottle_size is not None:
            try:
                record["bottle_size_ml"] = int(bottle_size)
            except (ValueError, TypeError):
                pass

        return record

    except Exception:
        return None


# ---------------------------------------------------------------------------
# Cache loading
# ---------------------------------------------------------------------------

def load_from_cache() -> list[dict]:
    """Load all cached HTML pages and extract raw product objects."""
    pages = sorted(CACHE_DIR.glob("page_*.html"))
    if not pages:
        print(
            f"No cache files found in {CACHE_DIR}. Run fetch.py first.",
            file=sys.stderr,
        )
        sys.exit(1)

    all_products = []
    for p in pages:
        html = p.read_text(encoding="utf-8")
        payload = reconstruct_rsc_payload(html)
        products = extract_products(payload)
        all_products.extend(products)
        print(
            f"  [parse] {p.name}: {len(products)} products extracted",
            file=sys.stderr,
        )

    print(
        f"Loaded {len(all_products)} raw product objects from {len(pages)} cache files",
        file=sys.stderr,
    )
    return all_products


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def parse_all(raw_products: list[dict]) -> list[dict]:
    wines = []
    skipped = 0
    seen_skus: set[str] = set()

    for raw in raw_products:
        record = parse_product(raw)
        if record is None:
            skipped += 1
            continue
        # Deduplicate by SKU (same variant may appear on multiple pages near
        # pagination boundaries, though recon says pages are disjoint)
        if record["sku"] in seen_skus:
            continue
        seen_skus.add(record["sku"])
        wines.append(record)

    print(
        f"Parsed {len(wines)} wine records ({skipped} skipped, "
        f"{len(raw_products) - skipped - len(wines)} duplicates)",
        file=sys.stderr,
    )
    return wines


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Parse flaschenpost.ch cached HTML into clean wine records"
    )
    parser.add_argument(
        "--from-cache",
        action="store_true",
        help="Load directly from cache/page_*.html (requires fetch.py to have run first)",
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
    else:
        print(
            "Error: --from-cache is required (pipe mode not supported — "
            "HTML files must come from the cache).",
            file=sys.stderr,
        )
        sys.exit(1)

    wines = parse_all(raw)

    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
