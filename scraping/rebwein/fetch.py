#!/usr/bin/env python3
"""
rebwein/fetch.py — cache-first fetcher for rebwein.ch (Laravel).

REB Wein is a server-rendered Laravel shop. The whole catalogue is the `/weine`
listing, paginated with ?page=N, ~21 product cards per page; the listing header
shows the total ("Anzahl Weine: N"). Each card already carries name, producer,
vintage, price and stock, so we only need the listing pages — no PDP fetches.

Usage
-----
    nix develop -c python scraping/rebwein/fetch.py                # fetch all (~19 pages)
    nix develop -c python scraping/rebwein/fetch.py --max-pages 3  # sample
    nix develop -c python scraping/rebwein/fetch.py --refresh
"""

import argparse
import math
import re
import sys
import time
from pathlib import Path

import requests

LIST_URL = "https://www.rebwein.ch/weine"
CACHE_DIR = Path.cwd() / "scraping" / "rebwein" / "cache"
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
    ),
    "Accept-Language": "de-CH,de;q=0.9,en;q=0.8",
}
DELAY_SECONDS = 1.0  # polite delay between live requests only

_CARD_MARKER = "wine-item"


def count_cards(html: str) -> int:
    return html.count('class="wine-item')


def detect_total_pages(html: str) -> int:
    per_page = count_cards(html) or 21
    m = re.search(r"Anzahl Weine:\s*([\d'’.]+)", html)
    if m:
        total = int(re.sub(r"[’'.]", "", m.group(1)))
        return max(1, math.ceil(total / per_page))
    return 1


def fetch_page(session: requests.Session, page: int, refresh: bool = False) -> str:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    cache_file = CACHE_DIR / f"page_{page:03d}.html"
    if cache_file.exists() and not refresh:
        print(f"  [cache] page {page} → {cache_file}", file=sys.stderr)
        return cache_file.read_text(encoding="utf-8")
    url = LIST_URL if page == 1 else f"{LIST_URL}?page={page}"
    print(f"  [fetch] page {page} ← {url}", file=sys.stderr)
    resp = session.get(url, timeout=30)
    resp.raise_for_status()
    html = resp.text
    cache_file.write_text(html, encoding="utf-8")
    print(f"         saved → {cache_file} ({count_cards(html)} cards)", file=sys.stderr)
    return html


def fetch_all(refresh: bool = False, max_pages: int | None = None) -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    session = requests.Session()
    session.headers.update(HEADERS)

    cache_p1 = CACHE_DIR / "page_001.html"
    was_cached = cache_p1.exists() and not refresh
    html_p1 = fetch_page(session, 1, refresh=refresh)
    if not was_cached:
        time.sleep(DELAY_SECONDS)

    total_pages = detect_total_pages(html_p1)
    if max_pages is not None:
        total_pages = min(total_pages, max_pages)
    print(f"  [meta]  detected {total_pages} page(s)", file=sys.stderr)

    for page in range(2, total_pages + 1):
        cache_file = CACHE_DIR / f"page_{page:03d}.html"
        was_cached = cache_file.exists() and not refresh
        html = fetch_page(session, page, refresh=refresh)
        if count_cards(html) == 0:
            print(f"  [stop]  page {page} has 0 cards — end of catalogue", file=sys.stderr)
            cache_file.unlink(missing_ok=True)
            break
        if not was_cached:
            time.sleep(DELAY_SECONDS)

    print(f"Done. Cache at {CACHE_DIR}", file=sys.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Fetch rebwein.ch /weine listing pages to cache.")
    parser.add_argument("--refresh", action="store_true",
                        help="Ignore cached files and re-download from the live site.")
    parser.add_argument("--max-pages", type=int, metavar="N",
                        help="Stop after fetching N pages (useful during development).")
    args = parser.parse_args()
    fetch_all(refresh=args.refresh, max_pages=args.max_pages)
