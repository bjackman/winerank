#!/usr/bin/env python3
"""
realwines/fetch.py — cache-first fetcher for the realwines.ch WooCommerce Store API.

Strategy
--------
Each page of the /wp-json/wc/store/v1/products endpoint is saved as a raw JSON
file under ./cache/page_N.json.  On subsequent runs the cached file is used and
the network is never touched.  Delete individual cache files (or the whole cache/
directory) to force a re-fetch of specific pages.

Usage
-----
    python3 scraping/realwines/fetch.py            # fetch all pages, use cache where available
    python3 scraping/realwines/fetch.py --refresh  # ignore cache, re-download everything
"""

import argparse
import json
import sys
import time
from pathlib import Path

import requests

BASE_URL = "https://realwines.ch/wp-json/wc/store/v1/products"
PER_PAGE = 100
# Resolved from CWD so it works both in-tree and when baked into the Nix store.
CACHE_DIR = Path.cwd() / "scraping" / "realwines" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    )
}
DELAY_SECONDS = 1.5  # polite delay between live requests


def fetch_page(page: int, refresh: bool = False) -> list[dict]:
    """Return parsed JSON for a single API page, using cache when available."""
    CACHE_DIR.mkdir(exist_ok=True)
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
    """
    Fetch every page of the product catalogue.

    On the first run this hits the API once to discover the total page count
    (via the X-WP-TotalPages header), then fetches each page in order.
    Subsequent runs load everything from the local cache.
    """
    # Discover total pages — use page 1 (which we'll cache too)
    CACHE_DIR.mkdir(exist_ok=True)
    cache_p1 = CACHE_DIR / "page_001.json"
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
        # Cache page 1 data
        data_p1 = resp.json()
        cache_p1.write_text(json.dumps(data_p1, ensure_ascii=False, indent=2))
        print(
            f"  [meta]  {total_products} products across {total_pages} pages",
            file=sys.stderr,
        )

    print(
        f"Fetching {total_products} products across {total_pages} pages…",
        file=sys.stderr,
    )

    all_products: list[dict] = []
    for page in range(1, total_pages + 1):
        products = fetch_page(page, refresh=refresh)
        all_products.extend(products)
        # Only delay between live requests
        cache_file = CACHE_DIR / f"page_{page:03d}.json"
        if not cache_file.exists() or refresh:
            time.sleep(DELAY_SECONDS)

    print(f"Total loaded: {len(all_products)} products", file=sys.stderr)
    return all_products


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch realwines.ch catalogue to cache")
    parser.add_argument(
        "--refresh",
        action="store_true",
        help="Ignore cached files and re-download from the live API",
    )
    args = parser.parse_args()

    products = fetch_all(refresh=args.refresh)
    # Emit raw API data to stdout for piping into parse.py
    print(json.dumps(products, ensure_ascii=False, indent=2))
