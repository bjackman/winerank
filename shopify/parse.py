#!/usr/bin/env python3
"""
shopify/parse.py — parse cached Shopify /products.json pages into wine records.

Works for any Shopify wine merchant. One record per (wine) variant, so different
sizes / vintages of the same product become distinct rows. Non-wine items (gift
sets, wooden cases, accessories, spirits, food) are dropped by product_type.

Shopify limitation: vintage / region / grape are not first-class fields — they
live in the title, tags, option values, or metafields, and metafields are NOT in
products.json. We recover the vintage from the variant option / title; region &
grape are left for a future PDP/metafield pass.

Prices are decimal strings (e.g. "12.00"); we convert to integer minor units
(rappen) to match realwines/vinazion.

Usage
-----
    nix develop -c python shopify/parse.py --name vergani     --from-cache --output shopify/vergani.json
    nix develop -c python shopify/parse.py --name advanvinum  --from-cache --output shopify/advanvinum.json
"""

import argparse
import html
import json
import re
import sys
from pathlib import Path

# product_type values that are NOT wine — dropped. Everything else is kept, so
# new wine categories don't silently disappear.
NON_WINE_TYPES = {
    "geschenkset",
    "holzkisten",
    "zubehör",
    "spirituosen/liquor",
    "lebensmittel",
}

# Some non-wine items have an empty product_type but are flagged by a tag
# (e.g. AdvanVinum's olive oil / olives). Drop if any tag matches exactly.
NON_WINE_TAGS = {
    "lebensmittel",
    "olivenöl",
    "öl",
    "oliven",
    "olive oil",
    "oil",
}


def cache_dir(name: str) -> Path:
    return Path.cwd() / "shopify" / "cache" / name


def load_from_cache(name: str) -> list[dict]:
    pages = sorted(cache_dir(name).glob("page_*.json"))
    if not pages:
        print(f"No cache files for {name!r}. Run fetch.py first.", file=sys.stderr)
        sys.exit(1)
    products: list[dict] = []
    for p in pages:
        products.extend(json.loads(p.read_text(encoding="utf-8")).get("products", []))
    print(f"Loaded {len(products)} products from {len(pages)} cache files", file=sys.stderr)
    return products


def strip_html(text: str) -> str:
    text = re.sub(r"<[^>]+>", "", text or "")
    return html.unescape(text).strip()


def parse_vintage(*candidates: str | None) -> int | None:
    for c in candidates:
        if not c:
            continue
        m = re.search(r"\b(19|20)\d{2}\b", c)
        if m:
            return int(m.group(0))
    return None


def price_to_minor(value) -> int | None:
    """'12.00' / 12.0 → 1200 minor units (rappen)."""
    if value in (None, ""):
        return None
    try:
        return int(round(float(value) * 100))
    except (TypeError, ValueError):
        return None


def is_wine(product: dict) -> bool:
    if (product.get("product_type") or "").strip().lower() in NON_WINE_TYPES:
        return False
    tags = product.get("tags") or []
    if isinstance(tags, str):  # Shopify sometimes serialises tags as a CSV string
        tags = [t.strip() for t in tags.split(",")]
    return not any((t or "").strip().lower() in NON_WINE_TAGS for t in tags)


def parse_product(product: dict, merchant: str, shop: str) -> list[dict]:
    name = strip_html(product.get("title", ""))
    producer = (product.get("vendor") or "").strip() or None
    handle = product.get("handle")
    url = f"https://{shop}/products/{handle}" if handle else None

    records = []
    for v in product.get("variants", []) or [{}]:
        option = v.get("option1")
        records.append(
            {
                "merchant": merchant,
                "name": name,
                "producer": producer,
                "vintage": parse_vintage(option, name),
                "currency": "CHF",
                "price": price_to_minor(v.get("price")),  # minor units (rappen)
                "in_stock": v.get("available"),
                "sku": (v.get("sku") or None),
                "variant": (option if option not in (None, "Default Title", "N/A") else None),
                "url": url,
            }
        )
    return records


def parse_all(products: list[dict], merchant: str, shop: str) -> list[dict]:
    wines: list[dict] = []
    dropped = 0
    for p in products:
        if not is_wine(p):
            dropped += 1
            continue
        wines.extend(parse_product(p, merchant, shop))
    print(
        f"Parsed {len(wines)} variant records from {len(products) - dropped} wines "
        f"({dropped} non-wine products dropped)",
        file=sys.stderr,
    )
    return wines


# Map merchant name → its shop host (for building product URLs).
SHOPS = {
    "vergani": "www.vergani.ch",
    "advanvinum": "advanvinum-wein.ch",
}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Parse cached Shopify products into wine records")
    parser.add_argument("--name", required=True, help="Merchant name (cache key, e.g. vergani)")
    parser.add_argument("--shop", help="Shop host (defaults from known SHOPS map)")
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--from-cache", action="store_true", help="Load from ./shopify/cache/<name>/page_*.json")
    src.add_argument("--input", type=Path, metavar="FILE", help="Read raw products JSON array from file")
    parser.add_argument("--output", type=Path, metavar="FILE", help="Write output JSON here (default: stdout)")
    args = parser.parse_args()

    shop = args.shop or SHOPS.get(args.name)
    if not shop:
        print(f"Unknown shop for {args.name!r}; pass --shop", file=sys.stderr)
        sys.exit(2)

    if args.input:
        products = json.loads(args.input.read_text(encoding="utf-8"))
    elif args.from_cache:
        products = load_from_cache(args.name)
    else:
        print("Reading raw products JSON from stdin…", file=sys.stderr)
        products = json.load(sys.stdin)

    wines = parse_all(products, args.name, shop)
    output_json = json.dumps(wines, ensure_ascii=False, indent=2)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output_json, encoding="utf-8")
        print(f"Wrote {len(wines)} records → {args.output}", file=sys.stderr)
    else:
        print(output_json)
