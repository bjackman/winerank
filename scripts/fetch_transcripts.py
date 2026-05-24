#!/usr/bin/env python3
"""
fetch_transcripts.py

Fetch transcripts and descriptions for every video on a YouTube channel
and save them as JSON files (one per video).

Video discovery (choose one):
  --api-key KEY         YouTube Data API v3 key (recommended, free quota is generous)
  --video-ids-file FILE Plain-text file with one video ID per line (quick workaround)

When both are provided, --video-ids-file is used for the ID list and --api-key
is used to enrich each entry with title, description, and audio language.

Subtitle selection priority:
  1. Manually created subtitles in the video's native language
  2. Auto-generated subtitles in the video's native language
  3. First available subtitle track (any language) — logged as a warning
"""

import argparse
import json
import sys
from pathlib import Path

import requests
from youtube_transcript_api import (
    YouTubeTranscriptApi,
    NoTranscriptFound,
    TranscriptsDisabled,
)

YOUTUBE_API_BASE = "https://www.googleapis.com/youtube/v3"


# ---------------------------------------------------------------------------
# YouTube Data API v3 helpers
# ---------------------------------------------------------------------------

def api_get(endpoint: str, api_key: str, **params) -> dict:
    """Make a YouTube Data API v3 GET request and return parsed JSON."""
    resp = requests.get(
        f"{YOUTUBE_API_BASE}/{endpoint}",
        params={"key": api_key, **params},
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def get_uploads_playlist_id(channel_handle: str, api_key: str) -> str:
    """Resolve a channel handle (e.g. @Foo) to its uploads playlist ID."""
    data = api_get("channels", api_key,
                   part="contentDetails",
                   forHandle=channel_handle)
    items = data.get("items", [])
    if not items:
        raise ValueError(
            f"Channel {channel_handle!r} not found via API. "
            "Check the handle spelling and that your API key is valid."
        )
    return items[0]["contentDetails"]["relatedPlaylists"]["uploads"]


def get_playlist_video_ids(playlist_id: str, api_key: str, limit: int | None) -> list[str]:
    """Page through an uploads playlist and return all video IDs in order."""
    video_ids: list[str] = []
    page_token: str | None = None

    while True:
        params: dict = {
            "part": "contentDetails",
            "playlistId": playlist_id,
            "maxResults": 50,
        }
        if page_token:
            params["pageToken"] = page_token

        data = api_get("playlistItems", api_key, **params)
        for item in data.get("items", []):
            video_ids.append(item["contentDetails"]["videoId"])
            if limit and len(video_ids) >= limit:
                return video_ids

        page_token = data.get("nextPageToken")
        if not page_token:
            break

    return video_ids


def batch_get_video_metadata(video_ids: list[str], api_key: str) -> dict[str, dict]:
    """
    Fetch snippet metadata for a list of video IDs (50 at a time).
    Returns a dict keyed by video ID.
    """
    metadata: dict[str, dict] = {}
    for i in range(0, len(video_ids), 50):
        batch = video_ids[i : i + 50]
        data = api_get("videos", api_key, part="snippet", id=",".join(batch))
        for item in data.get("items", []):
            snippet = item["snippet"]
            metadata[item["id"]] = {
                "id": item["id"],
                "title": snippet.get("title", ""),
                "description": snippet.get("description", ""),
                # defaultAudioLanguage is the most reliable field; fall back to
                # defaultLanguage (the video content language tag).
                "language": (
                    snippet.get("defaultAudioLanguage")
                    or snippet.get("defaultLanguage")
                ),
            }
    return metadata


def get_videos_from_api(
    channel_handle: str, api_key: str, limit: int | None
) -> list[dict]:
    """Enumerate a channel's videos and return full metadata using the Data API."""
    print(f"[*] Resolving channel {channel_handle!r} …", file=sys.stderr)
    uploads_id = get_uploads_playlist_id(channel_handle, api_key)
    print(f"[*] Uploads playlist: {uploads_id}", file=sys.stderr)

    print("[*] Enumerating videos …", file=sys.stderr)
    video_ids = get_playlist_video_ids(uploads_id, api_key, limit)
    print(f"[*] Found {len(video_ids)} video(s). Fetching metadata …", file=sys.stderr)

    metadata = batch_get_video_metadata(video_ids, api_key)

    # Preserve playlist order; stub out any deleted/private videos.
    return [
        metadata.get(vid_id, {"id": vid_id, "title": "", "description": "", "language": None})
        for vid_id in video_ids
    ]


def get_videos_from_file(ids_path: Path, api_key: str | None) -> list[dict]:
    """
    Load video IDs from a plain-text file (one per line, # comments allowed).
    If an api_key is provided, metadata is fetched for each ID; otherwise stubs
    are used (transcript will still be fetched, title/description left empty).
    """
    video_ids = [
        line.strip()
        for line in ids_path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.strip().startswith("#")
    ]
    print(f"[*] Loaded {len(video_ids)} video ID(s) from {ids_path}", file=sys.stderr)

    if api_key:
        print("[*] Fetching metadata via API …", file=sys.stderr)
        metadata = batch_get_video_metadata(video_ids, api_key)
        return [
            metadata.get(vid_id, {"id": vid_id, "title": "", "description": "", "language": None})
            for vid_id in video_ids
        ]

    return [
        {"id": vid_id, "title": "", "description": "", "language": None}
        for vid_id in video_ids
    ]


# ---------------------------------------------------------------------------
# Transcript fetching
# ---------------------------------------------------------------------------

def find_transcript(video_id: str, native_lang: str | None):
    """
    Select the best available transcript for a video.

    Priority:
      1. Manually created in native_lang
      2. Auto-generated in native_lang
      3. First available track in any language (with a warning)

    Returns (segments, kind, language_code).
    Raises NoTranscriptFound / TranscriptsDisabled on failure.
    """
    transcript_list = YouTubeTranscriptApi().list(video_id)

    if native_lang:
        # Priority 1 — manual in native language
        try:
            t = transcript_list.find_manually_created_transcript([native_lang])
            return t.fetch(), "manual", t.language_code
        except NoTranscriptFound:
            pass

        # Priority 2 — auto-generated in native language
        try:
            t = transcript_list.find_generated_transcript([native_lang])
            return t.fetch(), "auto", t.language_code
        except NoTranscriptFound:
            pass

    # Priority 3 — fallback: first available in any language
    for t in transcript_list:
        kind = "manual" if not t.is_generated else "auto"
        print(
            f"[!] {video_id}: no transcript in {native_lang!r}, "
            f"falling back to {t.language_code!r} ({kind})",
            file=sys.stderr,
        )
        return t.fetch(), kind, t.language_code

    raise NoTranscriptFound(video_id, [])


def process_video(
    video: dict,
    output_dir: Path,
    channel_handle: str,
    skip_existing: bool,
) -> None:
    video_id = video.get("id")
    if not video_id:
        print("[!] Skipping entry with no id.", file=sys.stderr)
        return

    out_path = output_dir / f"{video_id}.json"

    if skip_existing and out_path.exists():
        print(f"[=] {video_id}: already exists, skipping.", file=sys.stderr)
        return

    title = video.get("title", "")
    description = video.get("description", "")
    native_lang = video.get("language") or None

    print(f"[>] {video_id}: {title!r} (native_lang={native_lang!r})", file=sys.stderr)

    record: dict = {
        "id": video_id,
        "title": title,
        "description": description,
        "channel": channel_handle,
        "native_language": native_lang,
        "transcript_language": None,
        "transcript_kind": None,
        "transcript": None,
        "error": None,
    }

    try:
        segments, kind, lang_code = find_transcript(video_id, native_lang)
        record["transcript_language"] = lang_code
        record["transcript_kind"] = kind
        record["transcript"] = [
            {"text": seg["text"], "start": seg["start"], "duration": seg["duration"]}
            for seg in segments
        ]
        print(
            f"[+] {video_id}: fetched {len(record['transcript'])} segment(s) "
            f"({kind}, {lang_code})",
            file=sys.stderr,
        )
    except TranscriptsDisabled:
        msg = "Transcripts are disabled for this video."
        print(f"[-] {video_id}: {msg}", file=sys.stderr)
        record["error"] = msg
    except NoTranscriptFound as exc:
        msg = str(exc)
        print(f"[-] {video_id}: {msg}", file=sys.stderr)
        record["error"] = msg
    except Exception as exc:  # noqa: BLE001
        # Network / unexpected errors — do NOT write the file so the video is
        # retried on the next run.
        print(f"[!] {video_id}: unexpected error — {exc}", file=sys.stderr)
        return

    out_path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        description=(
            "Fetch YouTube transcripts and descriptions for a channel "
            "and write one JSON file per video."
        ),
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--channel",
        default="@KonstantinBaumMasterOfWine",
        help="YouTube channel handle (e.g. @SomeChannel). Used with --api-key.",
    )
    parser.add_argument(
        "--api-key",
        default=None,
        metavar="KEY",
        help=(
            "YouTube Data API v3 key. Enables channel enumeration and metadata "
            "fetching. Get a free key at https://console.cloud.google.com/."
        ),
    )
    parser.add_argument(
        "--video-ids-file",
        default=None,
        metavar="FILE",
        type=Path,
        help=(
            "Plain-text file with one YouTube video ID per line (# = comment). "
            "Use this as a workaround if you don't have an API key yet — "
            "copy IDs from the channel page in your browser."
        ),
    )
    parser.add_argument(
        "--output-dir",
        default="./transcripts",
        help="Directory to write per-video JSON files into.",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=None,
        metavar="N",
        help="Stop after N videos (useful for testing).",
    )
    parser.add_argument(
        "--no-skip",
        action="store_true",
        help="Re-fetch even if the output file already exists.",
    )
    args = parser.parse_args()

    if not args.api_key and not args.video_ids_file:
        parser.error(
            "Provide at least one of --api-key or --video-ids-file.\n\n"
            "  --api-key KEY          Enumerate the full channel automatically (recommended).\n"
            "                         Get a free key at https://console.cloud.google.com/\n"
            "                         (enable the YouTube Data API v3).\n\n"
            "  --video-ids-file FILE  Quick workaround: paste video IDs from the channel\n"
            "                         page in your browser (one per line) into a text file."
        )

    output_dir = Path(args.output_dir).expanduser().resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    print(f"[*] Output directory: {output_dir}", file=sys.stderr)

    if args.video_ids_file:
        videos = get_videos_from_file(args.video_ids_file, args.api_key)
        if args.limit:
            videos = videos[: args.limit]
    else:
        try:
            videos = get_videos_from_api(args.channel, args.api_key, args.limit)
        except requests.HTTPError as exc:
            print(f"[!] YouTube API error: {exc}\n{exc.response.text}", file=sys.stderr)
            sys.exit(1)
        except ValueError as exc:
            print(f"[!] {exc}", file=sys.stderr)
            sys.exit(1)

    skip_existing = not args.no_skip
    for video in videos:
        process_video(video, output_dir, args.channel, skip_existing)

    print("[*] Done.", file=sys.stderr)


if __name__ == "__main__":
    main()
