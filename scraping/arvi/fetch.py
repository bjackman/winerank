#!/usr/bin/env python3
"""
arvi/fetch.py — cache-first fetcher for the arvi.ch nopCommerce catalogue.

Arvi runs nopCommerce (.NET).  There is no standard public API, so we scrape
the wine category listing pages.  nopCommerce uses ``?pagenumber=N`` for
pagination.  Each HTML page is saved under ./cache/page_NNN.html and reused
on subsequent runs.

Pagination detection: nopCommerce puts a ``pager`` element in the page whose
last page-link gives the total page count.  We look for the highest
``?pagenumber=N`` value in the pager block and iterate up to that.

Usage
-----
    nix develop -c python scraping/arvi/fetch.py                    # fetch all, use cache
    nix develop -c python scraping/arvi/fetch.py --refresh          # ignore cache, re-download
    nix develop -c python scraping/arvi/fetch.py --max-pages 3      # stop after 3 pages (dev)

NOTE
----
The live site (arvi.ch) was unreachable during implementation (the egress
gateway returned 403 host_not_allowed).  The URL patterns and HTML structure
below follow the standard nopCommerce conventions but have NOT been verified
against a live response.  Adjust WINE_CATEGORY_URL and the HTML parsing regex
patterns in parse.py once the site is accessible.
"""

import argparse
import re
import sys
import time
from pathlib import Path

import requests

# nopCommerce wine/shop category listing URL.
# Typical patterns: /en/wine, /en/wines, /en/catalog, /en/shop, or a named
# category like /en/vins, /en/weine.  Adjust if the actual path differs.
WINE_CATEGORY_URL = "https://arvi.ch/en/wine"

CACHE_DIR = Path.cwd() / "scraping" / "arvi" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
    "Accept-Language": "en-US,en;q=0.5",
}
DELAY_SECONDS = 1.5  # polite delay between live requests only


# ---------------------------------------------------------------------------
# Pagination detection
# ---------------------------------------------------------------------------

def detect_total_pages(html: str) -> int:
    """
    Detect the total number of pages from a nopCommerce listing page.

    nopCommerce renders a ``<div class="pager">`` block containing links like
    ``?pagenumber=N``.  We find the highest N.

    Falls back to 1 if no pager is found (single-page catalogue or
    pagenumber param not found in HTML).
    """
    # Find all pagenumber values in the page
    page_nums = re.findall(r'[?&]pagenumber=(\d+)', html)
    if page_nums:
        return max(int(n) for n in page_nums)

    # nopCommerce also sometimes renders total pages as text like "Page 1 of N"
    m = re.search(r'[Pp]age\s+\d+\s+of\s+(\d+)', html)
    if m:
        return int(m.group(1))

    return 1


# ---------------------------------------------------------------------------
# Single-page fetch
# ---------------------------------------------------------------------------

def page_url(page: int) -> str:
    """Build the URL for a given page number."""
    if page == 1:
        return WINE_CATEGORY_URL
    return f"{WINE_CATEGORY_URL}?pagenumber={page}"


def fetch_page(page: int, refresh: bool = False) -> str:
    """Return raw HTML for one catalogue page, using cache when available."""
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
    print(f"         saved → {cache_file} ({len(html)} bytes)", file=sys.stderr)
    return html


# ---------------------------------------------------------------------------
# Full catalogue fetch
# ---------------------------------------------------------------------------

def fetch_all(refresh: bool = False, max_pages: int | None = None) -> None:
    """
    Fetch all pages of the wine catalogue, caching each as page_NNN.html.

    Page 1 is fetched first (or loaded from cache) so we can detect the total
    page count.  Subsequent pages are then fetched in order with a 1.5 s
    polite delay between live requests.
    """
    CACHE_DIR.mkdir(parents=True, exist_ok=True)

    # Fetch / load page 1 to discover pagination
    cache_p1 = CACHE_DIR / "page_001.html"
    was_cached = cache_p1.exists() and not refresh
    html_p1 = fetch_page(1, refresh=refresh)
    if not was_cached:
        time.sleep(DELAY_SECONDS)

    total_pages = detect_total_pages(html_p1)
    if max_pages is not None:
        total_pages = min(total_pages, max_pages)

    print(f"  [meta]  detected {total_pages} page(s)", file=sys.stderr)

    # Fetch remaining pages
    for page in range(2, total_pages + 1):
        cache_file = CACHE_DIR / f"page_{page:03d}.html"
        was_cached = cache_file.exists() and not refresh
        fetch_page(page, refresh=refresh)
        if not was_cached:
            time.sleep(DELAY_SECONDS)

    print(f"Done. {total_pages} page(s) in cache at {CACHE_DIR}", file=sys.stderr)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch arvi.ch wine catalogue pages to cache (nopCommerce)."
    )
    parser.add_argument(
        "--refresh",
        action="store_true",
        help="Ignore cached files and re-download from the live site.",
    )
    parser.add_argument(
        "--max-pages",
        type=int,
        metavar="N",
        help="Stop after fetching N pages (useful during development).",
    )
    args = parser.parse_args()

    fetch_all(refresh=args.refresh, max_pages=args.max_pages)
