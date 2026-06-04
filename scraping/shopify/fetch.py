#!/usr/bin/env python3
"""
shopify/fetch.py — cache-first fetcher for any Shopify storefront's public
/products.json endpoint. Used for every Shopify merchant (e.g. Vergani,
AdvanVinum) — pass the shop host and a short name.

Shopify exposes the full catalogue at /products.json?limit=250&page=N with no
auth. There is no total-count header, so we page until a page returns fewer than
`limit` products. Each page is cached as ./shopify/cache/<name>/page_NN.json.

Usage
-----
    nix develop -c python scraping/shopify/fetch.py --shop www.vergani.ch     --name vergani
    nix develop -c python scraping/shopify/fetch.py --shop advanvinum-wein.ch --name advanvinum
    nix develop -c python scraping/shopify/fetch.py --shop www.vergani.ch --name vergani --refresh
"""

import argparse
import json
import sys
import time
from pathlib import Path

import requests

LIMIT = 250  # Shopify's max page size for /products.json
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    )
}
DELAY_SECONDS = 1.5  # polite delay between live requests


def cache_dir(name: str) -> Path:
    # Resolved from CWD so it works both in-tree and when baked into the Nix store.
    return Path.cwd() / "scraping" / "shopify" / "cache" / name


def fetch_page(shop: str, name: str, page: int, refresh: bool = False) -> list[dict]:
    """Return the products list for a single page, using cache when available."""
    cdir = cache_dir(name)
    cdir.mkdir(parents=True, exist_ok=True)
    cache_file = cdir / f"page_{page:03d}.json"

    if cache_file.exists() and not refresh:
        print(f"  [cache] {name} page {page} → {cache_file}", file=sys.stderr)
        return json.loads(cache_file.read_text(encoding="utf-8")).get("products", [])

    url = f"https://{shop}/products.json?limit={LIMIT}&page={page}"
    print(f"  [fetch] {name} page {page} ← {url}", file=sys.stderr)
    resp = requests.get(url, headers=HEADERS, timeout=30)
    resp.raise_for_status()
    data = resp.json()
    cache_file.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    products = data.get("products", [])
    print(f"         saved → {cache_file} ({len(products)} products)", file=sys.stderr)
    return products


def fetch_all(shop: str, name: str, refresh: bool = False) -> list[dict]:
    """Page through /products.json until a short (or empty) page ends it."""
    all_products: list[dict] = []
    page = 1
    while True:
        cache_file = cache_dir(name) / f"page_{page:03d}.json"
        was_cached = cache_file.exists() and not refresh
        products = fetch_page(shop, name, page, refresh=refresh)
        all_products.extend(products)
        if not was_cached:
            time.sleep(DELAY_SECONDS)
        if len(products) < LIMIT:
            break
        page += 1

    print(f"Total loaded: {len(all_products)} products from {name}", file=sys.stderr)
    return all_products


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch a Shopify shop's /products.json to cache")
    parser.add_argument("--shop", required=True, help="Shop host, e.g. www.vergani.ch")
    parser.add_argument("--name", required=True, help="Short merchant name (cache/output key)")
    parser.add_argument("--refresh", action="store_true", help="Ignore cache and re-download")
    args = parser.parse_args()

    products = fetch_all(args.shop, args.name, refresh=args.refresh)
    print(json.dumps(products, ensure_ascii=False, indent=2))
