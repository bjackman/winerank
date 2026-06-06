#!/usr/bin/env python3
"""
landolt/fetch.py — cache-first fetcher for landolt-weine.ch (Shopware 6).

Landolt runs Shopware 6 (the storefront is served from landolt-weine.nextag.ch).
Product-detail pages are server-rendered with schema.org microdata, and every
product is listed in the gzipped Shopware sitemap as `…/<slug>/<number>`. So
this fetcher is sitemap-driven:

  1. GET /sitemap.xml (index) → the gzipped sitemap(s).
  2. Keep the product URLs (path ends `/<6+ digit number>`).
  3. Fetch each PDP and cache it as cache/<number>.html (cache-first).

Use --max-products to sample without pulling the whole ~700-page catalogue.

Usage
-----
    nix develop -c python scraping/landolt/fetch.py                    # fetch all (~690 pages)
    nix develop -c python scraping/landolt/fetch.py --max-products 20  # sample
    nix develop -c python scraping/landolt/fetch.py --refresh
"""

import argparse
import gzip
import re
import sys
import time
from pathlib import Path

import requests

SITEMAP_INDEX = "https://www.landolt-weine.ch/sitemap.xml"
CACHE_DIR = Path.cwd() / "scraping" / "landolt" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
}
DELAY_SECONDS = 1.0  # polite delay between live requests only

_PRODUCT_RE = re.compile(r"/\d{6,}$")


def product_number(url: str) -> str:
    return url.rstrip("/").rsplit("/", 1)[-1]


def product_urls(session: requests.Session, refresh: bool = False) -> list[str]:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    urls_file = CACHE_DIR / "_urls.txt"
    if urls_file.exists() and not refresh:
        return urls_file.read_text(encoding="utf-8").split()

    print(f"  [fetch] sitemap index ← {SITEMAP_INDEX}", file=sys.stderr)
    idx = session.get(SITEMAP_INDEX, timeout=30).text
    gz_urls = re.findall(r"<loc>([^<]+\.xml\.gz)</loc>", idx)
    if not gz_urls:
        print("ERROR: no .gz sitemap found in index", file=sys.stderr)
        sys.exit(1)

    urls: list[str] = []
    for gz in gz_urls:
        print(f"  [fetch] sitemap ← {gz}", file=sys.stderr)
        raw = session.get(gz, timeout=60).content
        if raw[:2] == b"\x1f\x8b":
            raw = gzip.decompress(raw)
        locs = re.findall(r"<loc>([^<]+)</loc>", raw.decode("utf-8", "replace"))
        urls += [u for u in locs if _PRODUCT_RE.search(u)]
    urls_file.write_text("\n".join(urls), encoding="utf-8")
    print(f"  [meta]  {len(urls)} product URLs in sitemap", file=sys.stderr)
    return urls


def fetch_pdp(session: requests.Session, url: str, refresh: bool = False) -> bool:
    cache_file = CACHE_DIR / f"{product_number(url)}.html"
    if cache_file.exists() and not refresh:
        return False
    resp = session.get(url, timeout=30)
    resp.raise_for_status()
    cache_file.write_text(resp.text, encoding="utf-8")
    return True


def fetch_all(refresh: bool = False, max_products: int | None = None) -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    session = requests.Session()
    session.headers.update(HEADERS)

    urls = product_urls(session, refresh=refresh)
    if max_products is not None:
        urls = urls[:max_products]

    print(f"Fetching {len(urls)} product pages…", file=sys.stderr)
    for i, url in enumerate(urls, 1):
        try:
            was_live = fetch_pdp(session, url, refresh=refresh)
        except requests.HTTPError as exc:
            print(f"  [skip] {url} ({exc})", file=sys.stderr)
            continue
        print(f"  [{'fetch' if was_live else 'cache'}] ({i}/{len(urls)}) {product_number(url)}", file=sys.stderr)
        if was_live:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache at {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch landolt-weine.ch PDPs to cache (sitemap-driven).")
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download (incl. the sitemap URL list).")
    parser.add_argument("--max-products", type=int, metavar="N",
                        help="Fetch only the first N product URLs (useful during development).")
    args = parser.parse_args()
    fetch_all(refresh=args.refresh, max_products=args.max_products)
