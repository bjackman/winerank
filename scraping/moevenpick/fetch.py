#!/usr/bin/env python3
"""
moevenpick/fetch.py — cache-first fetcher for moevenpick-wein.com (Magento).

Mövenpick is Magento. Its category pages don't expose a single clean "all wines"
listing, but every product is enumerated in the Magento XML sitemap and each
product-detail page (PDP) embeds rich JSON-LD (name, per-format offers with
price/currency/availability). So this fetcher is sitemap-driven:

  1. GET the de-CH sitemap and extract the product URLs (……/<slug>.html).
  2. Fetch each PDP and cache it as cache/<slug>.html (cache-first).

Non-product .html pages in the sitemap (CMS/landing pages) are fetched too but
drop out at parse time because they carry no JSON-LD Product block. Use
--max-products to sample without pulling the whole ~6k-page catalogue.

Usage
-----
    nix develop -c python scraping/moevenpick/fetch.py                    # fetch all (slow: ~6k pages)
    nix develop -c python scraping/moevenpick/fetch.py --max-products 20  # sample
    nix develop -c python scraping/moevenpick/fetch.py --refresh
"""

import argparse
import re
import sys
import time
from pathlib import Path

import requests

SITEMAP_URL = "https://www.moevenpick-wein.com/media/sitemap/sitemap_ch_de.xml"
CACHE_DIR = Path.cwd() / "scraping" / "moevenpick" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
}
DELAY_SECONDS = 1.0  # polite delay between live requests only


def slug_for(url: str) -> str:
    """Cache filename stem from a product URL (…/<slug>.html)."""
    return url.rstrip("/").rsplit("/", 1)[-1].removesuffix(".html")


def product_urls(refresh: bool = False) -> list[str]:
    """Return all product (.html) URLs from the sitemap, cached as _urls.txt."""
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    urls_file = CACHE_DIR / "_urls.txt"
    if urls_file.exists() and not refresh:
        return urls_file.read_text(encoding="utf-8").split()

    print(f"  [fetch] sitemap ← {SITEMAP_URL}", file=sys.stderr)
    resp = requests.get(SITEMAP_URL, headers=HEADERS, timeout=60)
    resp.raise_for_status()
    locs = re.findall(r"<loc>([^<]+)</loc>", resp.text)
    urls = [u for u in locs if u.endswith(".html")]
    # The sitemap lists category/landing .html pages (short slugs) before the
    # actual products. Sort the wine-looking slugs (vintage + multi-word name)
    # first so --max-products samples real products; the parser still filters by
    # the JSON-LD Product block, so non-products that slip through are dropped
    # and NV wines without a year prefix are still picked up on a full run.
    likely_product = re.compile(r"/de/\d{4}-.+-.+-").search
    urls.sort(key=lambda u: 0 if likely_product(u) else 1)
    urls_file.write_text("\n".join(urls), encoding="utf-8")
    print(f"  [meta]  {len(urls)} .html URLs in sitemap", file=sys.stderr)
    return urls


def fetch_pdp(url: str, refresh: bool = False) -> bool:
    """Fetch one PDP into cache/<slug>.html. Returns True if a live fetch happened."""
    cache_file = CACHE_DIR / f"{slug_for(url)}.html"
    if cache_file.exists() and not refresh:
        return False
    resp = requests.get(url, headers=HEADERS, timeout=30)
    resp.raise_for_status()
    cache_file.write_text(resp.text, encoding="utf-8")
    return True


def fetch_all(refresh: bool = False, max_products: int | None = None) -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    urls = product_urls(refresh=refresh)
    if max_products is not None:
        urls = urls[:max_products]

    print(f"Fetching {len(urls)} product pages…", file=sys.stderr)
    for i, url in enumerate(urls, 1):
        try:
            was_live = fetch_pdp(url, refresh=refresh)
        except requests.HTTPError as exc:
            print(f"  [skip] {url} ({exc})", file=sys.stderr)
            continue
        tag = "fetch" if was_live else "cache"
        print(f"  [{tag}] ({i}/{len(urls)}) {slug_for(url)}", file=sys.stderr)
        if was_live:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache at {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch moevenpick-wein.com PDPs to cache (sitemap-driven).")
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download (incl. the sitemap URL list).")
    parser.add_argument("--max-products", type=int, metavar="N",
                        help="Fetch only the first N product URLs (useful during development).")
    args = parser.parse_args()
    fetch_all(refresh=args.refresh, max_products=args.max_products)
