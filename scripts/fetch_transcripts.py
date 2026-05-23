#!/usr/bin/env python3
"""
fetch_transcripts.py

Fetch transcripts and descriptions for every video on a YouTube channel
and save them as JSON files (one per video).

Subtitle selection priority:
  1. Manually created subtitles in the video's native language
  2. Auto-generated subtitles in the video's native language
  3. First available subtitle track (any language) — logged as a warning
"""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

from youtube_transcript_api import (
    YouTubeTranscriptApi,
    NoTranscriptFound,
    TranscriptsDisabled,
)


CHANNEL_BASE = "https://www.youtube.com"


def get_video_metadata(channel_handle: str, limit: int | None) -> list[dict]:
    """Use yt-dlp to enumerate all videos on the channel with metadata."""
    url = f"{CHANNEL_BASE}/{channel_handle}/videos"
    cmd = [
        "yt-dlp",
        "--flat-playlist",
        "--print", "%(.{id,title,description,language})j",
        url,
    ]
    if limit:
        cmd += ["--playlist-end", str(limit)]

    print(f"[*] Enumerating videos from {url} …", file=sys.stderr)
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)

    videos = []
    for line in result.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            videos.append(json.loads(line))
        except json.JSONDecodeError as exc:
            print(f"[!] Could not parse metadata line: {line!r} — {exc}", file=sys.stderr)

    print(f"[*] Found {len(videos)} video(s).", file=sys.stderr)
    return videos


def find_transcript(video_id: str, native_lang: str | None):
    """
    Return (transcript_list, kind_str) where transcript_list is a list of
    {text, start, duration} dicts and kind_str is 'manual' or 'auto'.

    Raises NoTranscriptFound / TranscriptsDisabled on failure.
    """
    transcript_list = YouTubeTranscriptApi.list_transcripts(video_id)

    # Priority 1 — manual in native language
    if native_lang:
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

    # Priority 3 — fallback: any available transcript
    for t in transcript_list:
        kind = "manual" if not t.is_generated else "auto"
        print(
            f"[!] {video_id}: no transcript in '{native_lang}', "
            f"falling back to '{t.language_code}' ({kind})",
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
    native_lang = video.get("language") or None  # may be None if undeclared

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
        # youtube-transcript-api returns FetchedTranscript objects; convert to plain dicts
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
        # Network / unexpected errors — do NOT write the file so the video is retried.
        print(f"[!] {video_id}: unexpected error — {exc}", file=sys.stderr)
        return

    out_path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fetch YouTube transcripts and descriptions for a channel.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--channel",
        default="@KonstantinBaumMasterOfWine",
        help="YouTube channel handle (e.g. @SomeChannel).",
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
        help="Stop after fetching N videos (useful for testing).",
    )
    parser.add_argument(
        "--no-skip",
        action="store_true",
        help="Re-fetch even if the output file already exists.",
    )
    args = parser.parse_args()

    output_dir = Path(args.output_dir).expanduser().resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    print(f"[*] Output directory: {output_dir}", file=sys.stderr)

    try:
        videos = get_video_metadata(args.channel, args.limit)
    except subprocess.CalledProcessError as exc:
        print(f"[!] yt-dlp failed:\n{exc.stderr}", file=sys.stderr)
        sys.exit(1)

    skip_existing = not args.no_skip
    for video in videos:
        process_video(video, output_dir, args.channel, skip_existing)

    print("[*] Done.", file=sys.stderr)


if __name__ == "__main__":
    main()
