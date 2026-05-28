import argparse
import json
import os
import re
import sys
import time
import yt_dlp
from youtube_transcript_api import YouTubeTranscriptApi

def extract_video_id(url_or_id: str) -> str:
    if re.match(r'^[A-Za-z0-9_-]{11}$', url_or_id):
        return url_or_id

    patterns = [
        r'(?:v=|\/v\/|embed\/|youtu\.be\/|shorts\/)([A-Za-z0-9_-]{11})'
    ]
    for pattern in patterns:
        match = re.search(pattern, url_or_id)
        if match:
            return match.group(1)

    return url_or_id

def format_channel_url(url: str) -> str:
    url = url.rstrip('/')
    if url.endswith('/videos'):
        return url
    if '/@' in url or '/channel/' in url or '/c/' in url or '/user/' in url:
        return f"{url}/videos"
    return url

def get_recent_video_ids(channel_url: str, num_vids: int) -> list[str]:
    url = format_channel_url(channel_url)
    ydl_opts = {
        'quiet': True,
        'no_warnings': True,
        'extract_flat': True,
        'playlist_items': f'1-{num_vids}'
    }
    with yt_dlp.YoutubeDL(ydl_opts) as ydl:
        info = ydl.extract_info(url, download=False)
        entries = info.get('entries', [])
        return [entry.get('id') for entry in entries if entry.get('id')]

def fetch_video_description(video_id: str) -> str:
    ydl_opts = {
        'quiet': True,
        'no_warnings': True,
    }
    with yt_dlp.YoutubeDL(ydl_opts) as ydl:
        info = ydl.extract_info(video_id, download=False)
        return info.get('description', '')

class RateLimitedTranscriptApi:
    def __init__(self, delay: float = 2.0):
        self.delay = delay
        self.last_call_time = 0.0
        self.api = YouTubeTranscriptApi()

    def fetch(self, video_id: str):
        elapsed = time.time() - self.last_call_time
        if elapsed < self.delay:
            sleep_time = self.delay - elapsed
            print(f"Rate limiting: sleeping for {sleep_time:.2f} seconds...", file=sys.stderr)
            time.sleep(sleep_time)
        try:
            return self.api.fetch(video_id)
        finally:
            self.last_call_time = time.time()

def main():
    parser = argparse.ArgumentParser(description="Fetch YouTube channel video transcripts.")
    parser.add_argument(
        "channel_url",
        nargs="?",
        default="https://www.youtube.com/@KonstantinBaumMasterofWine",
        help="YouTube channel URL (default: https://www.youtube.com/@KonstantinBaumMasterofWine)."
    )
    parser.add_argument(
        "--num-vids",
        type=int,
        default=3,
        help="Number of most recent videos to download (default: 3)."
    )
    args = parser.parse_args()

    os.makedirs("transcripts", exist_ok=True)

    print(f"Fetching metadata for channel: {args.channel_url}...", file=sys.stderr)
    try:
        video_ids = get_recent_video_ids(args.channel_url, args.num_vids)
    except Exception as e:
        print(f"Error fetching channel metadata: {e}", file=sys.stderr)
        sys.exit(1)

    print(f"Found {len(video_ids)} video(s). Processing...", file=sys.stderr)

    ytt_api = RateLimitedTranscriptApi(delay=5.0)

    for video_id in video_ids:
        out_path = os.path.join("transcripts", f"{video_id}.json")

        if os.path.exists(out_path):
            print(f"Transcript for {video_id} already exists at {out_path}. Skipping download.", file=sys.stderr)
            continue

        print(f"Processing video {video_id}...", file=sys.stderr)
        try:
            description = fetch_video_description(video_id)
            transcript = ytt_api.fetch(video_id)

            texts = []
            for entry in transcript:
                if isinstance(entry, dict):
                    texts.append(entry.get('text', ''))
                else:
                    texts.append(str(getattr(entry, 'text', entry)))

            full_text = " ".join(texts)
            full_text = re.sub(r'\s+', ' ', full_text).strip()

            data = {
                "transcript": full_text,
                "description": description
            }

            with open(out_path, "w", encoding="utf-8") as f:
                json.dump(data, f, ensure_ascii=False, indent=2)

            print(f"Successfully saved transcript and description to {out_path}", file=sys.stderr)
        except Exception as e:
            print(f"Error processing video {video_id}: {e}", file=sys.stderr)

if __name__ == "__main__":
    main()
