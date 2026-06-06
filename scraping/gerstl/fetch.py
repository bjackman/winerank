#!/usr/bin/env python3
"""
gerstl/fetch.py — cache-first fetcher for gerstl.ch (Angular SPA).

Gerstl is an Angular app (Google-hosted). Pages are JS-rendered, BUT each
product-detail page ships its data in a server-side Angular TransferState blob
(`<script id="ng-state" type="application/json">…</script>`) embedded in the
static HTML — so no browser is needed. Every product is listed in the product
sitemap. This fetcher:

  1. GET the product sitemap (on the CDN) → product URLs (…/<slug>/p).
  2. Fetch each PDP and cache it as cache/<slug>.html (cache-first).

Use --max-products to sample without pulling the whole ~7500-page catalogue.

Usage
-----
    nix develop -c python scraping/gerstl/fetch.py                    # fetch all (~7500 pages)
    nix develop -c python scraping/gerstl/fetch.py --max-products 20  # sample
    nix develop -c python scraping/gerstl/fetch.py --refresh
"""

import argparse
import re
import sys
import time
from pathlib import Path

import requests

SITEMAP_URL = "https://cdn.gerstl.ch/seo/sitemap-products.xml"
CACHE_DIR = Path.cwd() / "scraping" / "gerstl" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
}
DELAY_SECONDS = 1.0  # polite delay between live requests only


def slug_for(url: str) -> str:
    """Product slug from …/<slug>/p (the segment before the trailing /p)."""
    parts = [p for p in url.split("/") if p]
    return parts[-2] if parts[-1] == "p" and len(parts) >= 2 else parts[-1]


def product_urls(session: requests.Session, refresh: bool = False) -> list[str]:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    urls_file = CACHE_DIR / "_urls.txt"
    if urls_file.exists() and not refresh:
        return urls_file.read_text(encoding="utf-8").split()

    print(f"  [fetch] sitemap ← {SITEMAP_URL}", file=sys.stderr)
    xml = session.get(SITEMAP_URL, timeout=60).text
    urls = [u for u in re.findall(r"<loc>([^<]+)</loc>", xml) if u.rstrip("/").endswith("/p")]
    urls_file.write_text("\n".join(urls), encoding="utf-8")
    print(f"  [meta]  {len(urls)} product URLs in sitemap", file=sys.stderr)
    return urls


def fetch_pdp(session: requests.Session, url: str, refresh: bool = False) -> bool:
    cache_file = CACHE_DIR / f"{slug_for(url)}.html"
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
        print(f"  [{'fetch' if was_live else 'cache'}] ({i}/{len(urls)}) {slug_for(url)}", file=sys.stderr)
        if was_live:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache at {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch gerstl.ch PDPs to cache (sitemap-driven).")
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download (incl. the sitemap URL list).")
    parser.add_argument("--max-products", type=int, metavar="N",
                        help="Fetch only the first N product URLs (useful during development).")
    args = parser.parse_args()
    fetch_all(refresh=args.refresh, max_products=args.max_products)
