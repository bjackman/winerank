#!/usr/bin/env python3
"""
passeur/fetch.py — cache-first fetcher for the lepasseurdevin.ch WooCommerce
Store API.

Le Passeur de Vin (lepasseurdevin.ch — note the "le"; the README's
passeurdevin.ch does not resolve) runs WooCommerce, so this is the same approach
as vinazion/realwines: each page of /wp-json/wc/store/v1/products is saved as raw
JSON under ./cache/page_NNN.json and reused on subsequent runs.

Usage
-----
    nix develop -c python scraping/passeur/fetch.py            # fetch all, use cache
    nix develop -c python scraping/passeur/fetch.py --refresh  # ignore cache
"""

import argparse
import json
import sys
import time
from pathlib import Path

import requests

BASE_URL = "https://www.lepasseurdevin.ch/wp-json/wc/store/v1/products"
PER_PAGE = 100
CACHE_DIR = Path.cwd() / "scraping" / "passeur" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    )
}
DELAY_SECONDS = 1.5  # polite delay between live requests


def fetch_page(page: int, refresh: bool = False) -> list[dict]:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    cache_file = CACHE_DIR / f"page_{page:03d}.json"
    if cache_file.exists() and not refresh:
        print(f"  [cache] page {page} → {cache_file}", file=sys.stderr)
        return json.loads(cache_file.read_text(encoding="utf-8"))

    url = f"{BASE_URL}?per_page={PER_PAGE}&page={page}&orderby=id&order=asc"
    print(f"  [fetch] page {page} ← {url}", file=sys.stderr)
    resp = requests.get(url, headers=HEADERS, timeout=30)
    resp.raise_for_status()
    data = resp.json()
    cache_file.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"         saved → {cache_file} ({len(data)} products)", file=sys.stderr)
    return data


def fetch_all(refresh: bool = False) -> list[dict]:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    meta_file = CACHE_DIR / "_meta.json"

    if meta_file.exists() and not refresh:
        meta = json.loads(meta_file.read_text())
        total_pages = meta["total_pages"]
        total_products = meta["total_products"]
    else:
        print("  [fetch] probing page 1 for pagination metadata…", file=sys.stderr)
        url = f"{BASE_URL}?per_page={PER_PAGE}&page=1&orderby=id&order=asc"
        resp = requests.get(url, headers=HEADERS, timeout=30)
        resp.raise_for_status()
        total_pages = int(resp.headers.get("X-WP-TotalPages", 1))
        total_products = int(resp.headers.get("X-WP-Total", 0))
        meta = {"total_pages": total_pages, "total_products": total_products}
        meta_file.write_text(json.dumps(meta, indent=2))
        (CACHE_DIR / "page_001.json").write_text(
            json.dumps(resp.json(), ensure_ascii=False, indent=2), encoding="utf-8"
        )
        print(f"  [meta]  {total_products} products across {total_pages} pages", file=sys.stderr)

    print(f"Fetching {total_products} products across {total_pages} pages…", file=sys.stderr)
    all_products: list[dict] = []
    for page in range(1, total_pages + 1):
        cache_file = CACHE_DIR / f"page_{page:03d}.json"
        was_cached = cache_file.exists() and not refresh
        all_products.extend(fetch_page(page, refresh=refresh))
        if not was_cached:
            time.sleep(DELAY_SECONDS)

    print(f"Total loaded: {len(all_products)} products", file=sys.stderr)
    return all_products


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch lepasseurdevin.ch catalogue to cache")
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download from the live API")
    args = parser.parse_args()
    products = fetch_all(refresh=args.refresh)
    print(json.dumps(products, ensure_ascii=False, indent=2))
