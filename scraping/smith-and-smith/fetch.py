#!/usr/bin/env python3
"""
smith-and-smith/fetch.py — cache-first fetcher for smithandsmith.ch.

Despite the README's first-pass guess, Smith & Smith is NOT nopCommerce. It's a
custom React/ASP.NET (Microsoft-IIS) shop. The full catalogue is the server-
rendered listing at /de/shop, paginated with ?page=N (1-indexed). Each page
holds 27 product cards; out-of-range pages return 0 cards. The listing shows a
total like `<div class="h4">2'984 Produkte</div>`, from which the page count is
derived.

Each page's raw HTML is saved under ./cache/page_NNN.html and reused on
subsequent runs. Delete cache files (or the cache/ dir) to force a re-fetch.

Usage
-----
    nix develop -c python scraping/smith-and-smith/fetch.py                # fetch all, use cache
    nix develop -c python scraping/smith-and-smith/fetch.py --refresh      # ignore cache, re-download
    nix develop -c python scraping/smith-and-smith/fetch.py --max-pages 3  # stop after 3 pages (dev)
"""

import argparse
import math
import re
import sys
import time
from pathlib import Path

import requests

SHOP_URL = "https://www.smithandsmith.ch/de/shop"
CACHE_DIR = Path.cwd() / "scraping" / "smith-and-smith" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
}
DELAY_SECONDS = 1.5  # polite delay between live requests only

_CARD_MARKER = "card artikel-box"


def count_cards(html: str) -> int:
    return html.count(_CARD_MARKER)


def detect_total_pages(html: str) -> int:
    """Derive page count from the "N Artikel" total and the per-page card count.

    Falls back to 1 if the total can't be found (fetch_all also iterates until a
    page returns 0 cards as a safety net)."""
    per_page = count_cards(html) or 27
    m = re.search(r'<div class="h4">(\d[\d.’\']*)\s*Produkte</div>', html)
    if m:
        total = int(re.sub(r"[.’']", "", m.group(1)))
        return max(1, math.ceil(total / per_page))
    return 1


def page_url(page: int) -> str:
    return SHOP_URL if page == 1 else f"{SHOP_URL}?page={page}"


def fetch_page(page: int, refresh: bool = False) -> str:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    cache_file = CACHE_DIR / f"page_{page:03d}.html"

    if cache_file.exists() and not refresh:
        print(f"  [cache] page {page} → {cache_file}", file=sys.stderr)
        return cache_file.read_text(encoding="utf-8")

    url = page_url(page)
    print(f"  [fetch] page {page} ← {url}", file=sys.stderr)
    resp = requests.get(url, headers=HEADERS, timeout=30)
    resp.raise_for_status()
    html = resp.text
    cache_file.write_text(html, encoding="utf-8")
    print(f"         saved → {cache_file} ({len(html)} bytes, {count_cards(html)} cards)", file=sys.stderr)
    return html


def fetch_all(refresh: bool = False, max_pages: int | None = None) -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)

    cache_p1 = CACHE_DIR / "page_001.html"
    was_cached = cache_p1.exists() and not refresh
    html_p1 = fetch_page(1, refresh=refresh)
    if not was_cached:
        time.sleep(DELAY_SECONDS)

    total_pages = detect_total_pages(html_p1)
    if max_pages is not None:
        total_pages = min(total_pages, max_pages)
    print(f"  [meta]  detected {total_pages} page(s)", file=sys.stderr)

    for page in range(2, total_pages + 1):
        cache_file = CACHE_DIR / f"page_{page:03d}.html"
        was_cached = cache_file.exists() and not refresh
        html = fetch_page(page, refresh=refresh)
        # Safety net: stop early if we run past the real last page.
        if count_cards(html) == 0:
            print(f"  [stop]  page {page} has 0 cards — end of catalogue", file=sys.stderr)
            cache_file.unlink(missing_ok=True)
            break
        if not was_cached:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache at {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch smithandsmith.ch /de/shop listing pages to cache."
    )
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download from the live site.")
    parser.add_argument("--max-pages", type=int, metavar="N",
                        help="Stop after fetching N pages (useful during development).")
    args = parser.parse_args()

    fetch_all(refresh=args.refresh, max_pages=args.max_pages)
