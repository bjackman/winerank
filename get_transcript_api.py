import argparse
import json
import re
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

def fetch_video_description(video_id: str) -> str:
    ydl_opts = {
        'quiet': True,
        'no_warnings': True,
    }
    with yt_dlp.YoutubeDL(ydl_opts) as ydl:
        info = ydl.extract_info(video_id, download=False)
        return info.get('description', '')

def main():
    parser = argparse.ArgumentParser(description="Fetch YouTube video transcript.")
    parser.add_argument(
        "video_input",
        help="YouTube video ID or full YouTube video URL."
    )
    args = parser.parse_args()

    video_id = extract_video_id(args.video_input)

    description = fetch_video_description(video_id)

    ytt_api = YouTubeTranscriptApi()
    transcript = ytt_api.fetch(video_id)

    texts = []
    for entry in transcript:
        if isinstance(entry, dict):
            texts.append(entry.get('text', ''))
        else:
            texts.append(str(getattr(entry, 'text', entry)))

    full_text = " ".join(texts)
    full_text = re.sub(r'\s+', ' ', full_text).strip()

    print(json.dumps({
        "transcript": full_text,
        "description": description
    }, ensure_ascii=False))

if __name__ == "__main__":
    main()

