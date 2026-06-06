#!/usr/bin/env python3
"""
flaschenpost/fetch.py — cache-first fetcher for flaschenpost.ch (Next.js/RSC).

Strategy
--------
The product catalogue is server-rendered into the page's RSC payload
(self.__next_f.push([1,"..."]) script chunks).  We fetch /wein?page=N,
reconstruct the RSC stream, and cache the raw HTML as cache/page_NNNN.html.

Page count is discovered by extracting "total":N from the RSC payload on
page 1, then computing ceil(total / 12) (12 products per page, fixed).

Usage
-----
    nix develop -c python scraping/flaschenpost/fetch.py
    nix develop -c python scraping/flaschenpost/fetch.py --refresh
    nix develop -c python scraping/flaschenpost/fetch.py --max-pages 5
"""

import argparse
import json
import math
import re
import sys
import time
from pathlib import Path

import requests

BASE_URL = "https://www.flaschenpost.ch/wein"
PER_PAGE = 12  # fixed server-side, not adjustable via query params
CACHE_DIR = Path(__file__).parent / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.5",
}
DELAY_SECONDS = 1.5  # polite delay between live requests


def reconstruct_rsc_payload(html: str) -> str:
    """Reconstruct the Next.js RSC stream payload from a page's HTML."""
    chunks = re.findall(r'self\.__next_f\.push\(\[1,(".*?")\]\)', html, re.S)
    return "".join(json.loads(c) for c in chunks)


def discover_page_count(html: str) -> tuple[int, int]:
    """
    Extract total product count from the RSC payload of page 1.
    Returns (total_products, page_count).
    """
    payload = reconstruct_rsc_payload(html)
    m = re.search(r'"total":(\d+)', payload)
    if not m:
        raise ValueError(
            'Could not find "total":N in RSC payload — site structure may have changed'
        )
    total = int(m.group(1))
    page_count = math.ceil(total / PER_PAGE)
    return total, page_count


def fetch_page(page: int, refresh: bool = False) -> str:
    """
    Return raw HTML for a single listing page, using cache when available.
    Returns the HTML string and a bool indicating whether a live fetch was made.
    """
    CACHE_DIR.mkdir(exist_ok=True)
    cache_file = CACHE_DIR / f"page_{page:04d}.html"

    if cache_file.exists() and not refresh:
        print(f"  [cache] page {page} → {cache_file}", file=sys.stderr)
        return cache_file.read_text(encoding="utf-8"), False

    url = f"{BASE_URL}?page={page}"
    print(f"  [fetch] page {page} ← {url}", file=sys.stderr)
    resp = requests.get(url, headers=HEADERS, timeout=30)
    resp.raise_for_status()

    html = resp.text
    cache_file.write_text(html, encoding="utf-8")
    print(
        f"         saved → {cache_file} ({len(html):,} bytes)",
        file=sys.stderr,
    )
    return html, True


def fetch_all(refresh: bool = False, max_pages: int | None = None) -> None:
    """
    Fetch every page of /wein and cache raw HTML.

    Discovers page count from page 1's RSC payload, then fetches pages 2..N.
    A polite 1.5 s delay is inserted between live (non-cached) fetches only.
    """
    CACHE_DIR.mkdir(exist_ok=True)

    # --- Page 1: discover total count ---
    html_p1, was_live = fetch_page(1, refresh=refresh)

    try:
        total_products, page_count = discover_page_count(html_p1)
    except ValueError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        sys.exit(1)

    if max_pages is not None:
        page_count = min(page_count, max_pages)

    print(
        f"  [meta]  {total_products:,} products → {page_count} pages"
        + (f" (capped at --max-pages {max_pages})" if max_pages else ""),
        file=sys.stderr,
    )
    print(
        f"Fetching {page_count} pages of /wein…",
        file=sys.stderr,
    )

    # Insert delay if page 1 was a live fetch and we have more pages to fetch
    if was_live and page_count > 1:
        time.sleep(DELAY_SECONDS)

    for page in range(2, page_count + 1):
        _, was_live = fetch_page(page, refresh=refresh)
        if was_live and page < page_count:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache: {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch flaschenpost.ch catalogue to cache"
    )
    parser.add_argument(
        "--refresh",
        action="store_true",
        help="Ignore cached files and re-download from the live site",
    )
    parser.add_argument(
        "--max-pages",
        type=int,
        metavar="N",
        default=None,
        help="Limit fetch to the first N pages (for testing)",
    )
    args = parser.parse_args()

    fetch_all(refresh=args.refresh, max_pages=args.max_pages)
