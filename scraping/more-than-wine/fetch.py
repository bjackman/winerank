#!/usr/bin/env python3
"""
more-than-wine/fetch.py — cache-first fetcher for the more-than-wine.com
PrestaShop wine category listing pages.

Strategy
--------
PrestaShop uses ``?p=N`` pagination on category pages.  We fetch
``<WINE_CATEGORY_URL>?p=N`` for each page N and save the raw HTML under
``./cache/page_NNN.html``.  On subsequent runs the cached file is used and
the network is never touched.  Delete individual cache files (or the whole
cache/ directory) to force a re-fetch.

Pagination detection: we look for the highest ``?p=N`` value in the pager
block of page 1, or fall back to parsing a "total products" counter and
dividing by the observed page size.

Usage
-----
    nix develop -c python scraping/more-than-wine/fetch.py
    nix develop -c python scraping/more-than-wine/fetch.py --refresh
    nix develop -c python scraping/more-than-wine/fetch.py --max-pages 3

NOTE
----
The live site (www.more-than-wine.com) was unreachable during implementation
(the egress gateway returned 403 host_not_allowed).  The URL patterns and
HTML structure below follow the standard PrestaShop conventions but have NOT
been verified against a live response.

Things to verify with live access:
  - Correct WINE_CATEGORY_URL slug (try /wein, /vin, /wine, /vins, /vins-naturels,
    or a numeric ?controller=category&id_category=N URL)
  - Pagination param (standard is ?p=N; some themes use ?page=N)
  - Products-per-page count (PrestaShop default: 12 or 24; try appending ?n=48)
  - Whether JSON-LD blocks are present in category pages (vs only on PDPs)
"""

import argparse
import re
import sys
import time
from pathlib import Path

import requests

# ---------------------------------------------------------------------------
# Configuration — adjust WINE_CATEGORY_URL after live verification
# ---------------------------------------------------------------------------

# PrestaShop wine category URL.  Typical slugs: /wein, /vin, /wine, /vins,
# /vins-naturels.  If the site uses the legacy controller URL it will look like
# ?controller=category&id_category=N — adjust accordingly.
WINE_CATEGORY_URL = "https://www.more-than-wine.com/wein"

# PrestaShop supports a ?n=N products-per-page override.  48 keeps page count low;
# if the site ignores it you'll just get the default (usually 12–24).
PRODUCTS_PER_PAGE = 48

CACHE_DIR = Path.cwd() / "scraping" / "more-than-wine" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept": (
        "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"
    ),
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.5",
}
DELAY_SECONDS = 1.5  # polite delay between live requests only


# ---------------------------------------------------------------------------
# Pagination detection
# ---------------------------------------------------------------------------

def detect_total_pages(html: str) -> int:
    """
    Detect total page count from a PrestaShop category listing page.

    Strategy 1: Find the highest ``?p=N`` value in pagination links.
    Strategy 2: Parse a "showing X of Y" / total-products counter and divide
                by the observed page-size.
    Strategy 3: Fall back to 1 (single-page catalogue).
    """
    # Strategy 1 — look for ?p=N in pagination links
    page_nums = re.findall(r'[?&]p=(\d+)', html)
    if page_nums:
        return max(int(n) for n in page_nums)

    # Strategy 2 — look for a total-products text block
    # PrestaShop themes may render "Showing 1–24 of 312 products" or
    # <span class="total-products">312</span>
    m = re.search(r'total[_-]?products["\s>]+(\d+)', html, re.I)
    if m:
        total = int(m.group(1))
        return max(1, -(-total // PRODUCTS_PER_PAGE))  # ceiling division

    # "of N" style: "1 - 24 of 312"
    m = re.search(r'\bof\s+(\d+)\b', html)
    if m:
        total = int(m.group(1))
        return max(1, -(-total // PRODUCTS_PER_PAGE))

    return 1


# ---------------------------------------------------------------------------
# Single-page fetch
# ---------------------------------------------------------------------------

def page_url(page: int) -> str:
    """Build the URL for a given page number."""
    if page == 1:
        return f"{WINE_CATEGORY_URL}?n={PRODUCTS_PER_PAGE}"
    return f"{WINE_CATEGORY_URL}?p={page}&n={PRODUCTS_PER_PAGE}"


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

    content = resp.text
    cache_file.write_text(content, encoding="utf-8")
    print(f"         saved → {cache_file} ({len(content)} bytes)", file=sys.stderr)
    return content


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

    print(
        f"Done. {total_pages} page(s) cached at {CACHE_DIR}",
        file=sys.stderr,
    )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch more-than-wine.com wine catalogue pages to cache (PrestaShop)."
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
