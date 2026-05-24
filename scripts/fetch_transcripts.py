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
import glob
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

import requests

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

def parse_vtt(vtt_content: str) -> list[dict]:
    """Parse WebVTT content and return a list of segments with text, start, and duration."""
    TIMESTAMP_RE = re.compile(
        r'(?:(\d{2}):)?(\d{2}):(\d{2})\.(\d{3}) --> (?:(\d{2}):)?(\d{2}):(\d{2})\.(\d{3})'
    )
    
    def parse_time(hours, minutes, seconds, milliseconds):
        h = int(hours) if hours else 0
        m = int(minutes)
        s = int(seconds)
        ms = int(milliseconds)
        return h * 3600 + m * 60 + s + ms / 1000.0

    segments = []
    content = vtt_content.replace('\r\n', '\n').replace('\r', '\n')
    blocks = content.split('\n\n')
    
    for block in blocks:
        lines = [line.strip() for line in block.split('\n') if line.strip()]
        if not lines:
            continue
            
        timestamp_line = None
        text_lines = []
        for line in lines:
            if ' --> ' in line:
                timestamp_line = line
            elif timestamp_line is not None:
                text_lines.append(line)
                
        if not timestamp_line:
            continue
            
        match = TIMESTAMP_RE.search(timestamp_line)
        if not match:
            continue
            
        sh, sm, ss, sms, eh, em, es, ems = match.groups()
        start = parse_time(sh, sm, ss, sms)
        end = parse_time(eh, em, es, ems)
        duration = round(end - start, 3)
        
        text = " ".join(text_lines)
        text = re.sub(r'<[^>]+>', '', text)
        text = re.sub(r'\{[^\}]+\}', '', text)
        text = re.sub(r'\s+', ' ', text).strip()
        
        if text:
            segments.append({
                "text": text,
                "start": round(start, 3),
                "duration": duration
            })
            
    # Simple clean up of rollup duplicates for auto-generated subs
    deduped = []
    for seg in segments:
        if not deduped:
            deduped.append(seg)
        else:
            prev = deduped[-1]
            if prev["text"] == seg["text"]:
                prev["duration"] = round(seg["start"] + seg["duration"] - prev["start"], 3)
            else:
                deduped.append(seg)
                
    return deduped


def fetch_transcript_with_yt_dlp(video_id: str, cookies_arg: str | None, native_lang: str | None) -> tuple[list[dict], str, str]:
    """
    Run yt-dlp to download subtitles and parse the resulting WebVTT.
    Returns (segments, kind, language_code).
    """
    import time
    import random

    langs = native_lang if native_lang else "en"
    max_retries = 4
    backoff_factor = 2.0
    initial_delay = 5.0
    
    for attempt in range(max_retries):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp_path = Path(tmpdir)
            
            cmd = [
                "yt-dlp",
                "--write-subs",
                "--write-auto-subs",
                "--skip-download",
                "--sub-format", "vtt",
                "--sub-langs", langs,
                "-o", str(tmp_path / "%(id)s.%(ext)s"),
                f"https://www.youtube.com/watch?v={video_id}"
            ]
            
            if cookies_arg:
                known_browsers = {"chrome", "firefox", "safari", "opera", "edge", "brave", "chromium", "vivaldi", "librewolf"}
                if cookies_arg.lower() in known_browsers:
                    cmd.extend(["--cookies-from-browser", cookies_arg.lower()])
                else:
                    cookies_file = Path(cookies_arg).expanduser().resolve()
                    cmd.extend(["--cookies", str(cookies_file)])
                    
            print(f"[*] Running yt-dlp subtitle download for {video_id} (attempt {attempt + 1}/{max_retries}) …", file=sys.stderr)
            
            result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=False)
            vtt_files = glob.glob(str(tmp_path / "*.vtt"))
            
            if not vtt_files and langs != ".*":
                print(f"[!] Subtitles not found for language {langs}, retrying with all languages …", file=sys.stderr)
                cmd_fallback = cmd.copy()
                idx = cmd_fallback.index("--sub-langs")
                cmd_fallback[idx + 1] = ".*"
                result = subprocess.run(cmd_fallback, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=False)
                vtt_files = glob.glob(str(tmp_path / "*.vtt"))
                
            if vtt_files:
                vtt_file = Path(vtt_files[0])
                parts = vtt_file.name.split('.')
                lang_code = parts[-2] if len(parts) >= 3 else "unknown"
                
                output_text = (result.stdout + result.stderr).lower()
                if "downloading automatic subtitles" in output_text or "auto-generated" in output_text:
                    kind = "auto"
                else:
                    kind = "manual"
                    
                vtt_content = vtt_file.read_text(encoding="utf-8")
                segments = parse_vtt(vtt_content)
                return segments, kind, lang_code
                
            stderr_text = result.stderr or ""
            if "Too Many Requests" in stderr_text or "429" in stderr_text:
                if attempt < max_retries - 1:
                    sleep_time = initial_delay * (backoff_factor ** attempt) + random.uniform(0.1, 1.0)
                    print(f"[!] YouTube returned HTTP 429 (Too Many Requests). Sleeping for {sleep_time:.2f}s before retrying …", file=sys.stderr)
                    time.sleep(sleep_time)
                    continue
                else:
                    raise RuntimeError("YouTube blocked the request (HTTP Error 429: Too Many Requests) after max retries.")
                    
            if "has no subtitles" in stderr_text or "does not have subtitles" in stderr_text or "does not contain" in stderr_text:
                raise ValueError("Transcripts are disabled or not found for this video.")
            raise ValueError(f"Failed to retrieve subtitles: {stderr_text.strip()}")


def process_video(
    cookies_arg: str | None,
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
        segments, kind, lang_code = fetch_transcript_with_yt_dlp(video_id, cookies_arg, native_lang)
        record["transcript_language"] = lang_code
        record["transcript_kind"] = kind
        record["transcript"] = segments
        print(
            f"[+] {video_id}: fetched {len(record['transcript'])} segment(s) "
            f"({kind}, {lang_code})",
            file=sys.stderr,
        )
    except ValueError as exc:
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
    parser.add_argument(
        "--cookies",
        default=None,
        metavar="FILE_OR_BROWSER",
        help="Path to a Netscape format cookies.txt file, or browser name to load from your browser profile.",
    )
    parser.add_argument(
        "--sleep",
        type=float,
        default=2.0,
        metavar="SECONDS",
        help="Number of seconds to sleep between video requests to avoid rate limits.",
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
    import time
    for idx, video in enumerate(videos):
        if idx > 0 and args.sleep > 0:
            print(f"[*] Sleeping for {args.sleep}s to avoid rate limits …", file=sys.stderr)
            time.sleep(args.sleep)
        process_video(args.cookies, video, output_dir, args.channel, skip_existing)

    print("[*] Done.", file=sys.stderr)


if __name__ == "__main__":
    main()
