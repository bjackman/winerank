package main

import (
	"fmt"
	"strings"
)

const tastingSystemPrompt = `You are a wine tasting segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video.
2. NUMBERED TRANSCRIPT: A numbered transcript from the first half of a blind wine tasting video.
   Each sentence is prefixed with a line number in brackets, e.g. [1], [2], etc.

Your task:
Count the number of wines listed in the DESCRIPTION.
Identify the exact sentence index where the reviewer begins tasting/discussing each placeholder wine (e.g. "Wine 1", "Wine 2", "wine number three", etc.) in the transcript.
You must find all tasting segments (e.g., Wine 1, Wine 2, Wine 3, etc.). The number of tasting segments should match the number of wines in the DESCRIPTION.
Each tasting starts when he pours the glass or takes the first sip, and runs continuously until the next wine tasting (or a sponsor ad) starts.

Return ONLY a JSON object with this schema:
{
  "tasting_segments": [
    {
      "placeholder": "Wine 1",
      "start": 26
    },
    {
      "placeholder": "Wine 2",
      "start": 48
    }
  ]
}

Rules:
- Be precise: Locate the exact sentence index where the topic shifts to a new wine.
- The "tasting_segments" array must be in chronological order by "start" index.
- Ensure you list all the tasting segments for all the wines in the DESCRIPTION. Do not stop after Wine 2.
- Return ONLY the JSON object, no other text.`

const revealSystemPrompt = `You are a wine reveal segmenter. You will be given:
1. DESCRIPTION: A list of wines reviewed in the video (use these exact names).
2. TASTING SUMMARY: A summarized transcript of the blind tasting phase (first half), detailing when each placeholder (Wine 1, Wine 2, etc.) was tasted and the rating scores or descriptors used.
3. NUMBERED TRANSCRIPT: A numbered transcript from the second half of the blind wine tasting video where the bottles are uncloaked/revealed.
   Each sentence is prefixed with a line number in brackets, e.g. [1], [2], etc.

Your task:
1. Match each tasting placeholder (e.g. "Wine 1", "Wine 2", "wine number three", etc.) to the actual wine name from the DESCRIPTION.
2. Identify the exact sentence index where the reviewer begins uncloaking/revealing each placeholder wine (typically in the second half of the video, e.g. "wine number one was...", "wine 2 was...").

To determine the exact start index of each reveal and the correct placeholder mappings, follow these clues:
- Cross-reference ratings and scores: If the reviewer says "I rated it 94 points" or "this is the highest rated wine with 95 points" in the reveal transcript, match this to the rating they assigned to the placeholder in the TASTING SUMMARY (e.g. if Wine 3 was rated 94 points in the TASTING SUMMARY, then the uncloaking of the 94 point wine corresponds to Wine 3).
- Cross-reference tasting guesses and region comments: Match references in the reveal phase back to region guesses and tasting styles in the TASTING SUMMARY (e.g. if they say "I thought this was from Paso... this is actually the Super Tuscan from Bulgery", and Wine 4 was guessed as Paso Robles in the TASTING SUMMARY, then Wine 4 maps to Castello di Bolgheri Varvara Bolgheri).
- Cross-reference bottle features and tasting notes: If the reviewer mentions "screw cap", "browning", or specific grapes/regions during the reveal, match it back to when those features were first discussed in the TASTING SUMMARY.
- Transition statements: Look for transition sentences like "All right, I feel better now.", "This is the wine with...", "Let's see what it is...", "So wine number X was..." as the starting point of the reveal.

Return ONLY a JSON object with this schema:
{
  "placeholder_mappings": [
    {
      "placeholder": "Wine 1",
      "wine_name": "Exact wine name from description"
    }
  ],
  "reveal_segments": [
    {
      "placeholder": "Wine 1",
      "start": 244
    },
    {
      "placeholder": "Wine 2",
      "start": 250
    }
  ]
}

Rules:
- Be precise: Locate the exact sentence index where the reviewer begins discussing the reveal of that wine (often starting with a transition like "All right, I feel better now. This is the wine with a screw cap.").
- The "reveal_segments" array must be in chronological order by "start" index.
- Return ONLY the JSON object, no other text.`


const extractSystemPrompt = `You are a wine review score extractor. You will be given:
1. WINE NAME: The name of the wine being reviewed.
2. TRANSCRIPT SEGMENT: A focused segment of a video transcript discussing this wine.

Your task is to extract:
1. The producer or winery name.
2. The vintage year or "NV" for non-vintage.
3. The region and country (e.g. "Paso Robles, USA" or "Bordeaux, France").
4. The numerical score (on the 100-point scale) assigned to this wine by the reviewer, if any.
5. A brief 1-2 sentence summary of the tasting notes.
6. A verbatim matching snippet of 1-2 sentences from the provided transcript segment where the review or score is discussed.

Return ONLY a JSON object with this schema:
{
  "producer": "Producer/winery name",
  "vintage": "Vintage year or NV",
  "region": "Region, country",
  "score": 91,
  "notes_summary": "Brief 1-2 sentence summary of the tasting notes.",
  "matching_snippet": "Verbatim quote from the transcript segment."
}

Rules:
- Set "score" to null if no numerical score is explicitly assigned.
- The "matching_snippet" must be an exact verbatim substring from the transcript segment provided.
- Infer the producer, vintage, and region from the WINE NAME or TRANSCRIPT SEGMENT.
- Return ONLY the JSON object, no other text.`

func BuildSegmentUserMessage(description, numberedTranscript string) string {
	return fmt.Sprintf("DESCRIPTION:\n%s\n\nNUMBERED TRANSCRIPT:\n%s", cleanDescription(description), numberedTranscript)
}

func BuildTastingUserMessage(description, numberedTranscript string) string {
	return fmt.Sprintf("DESCRIPTION:\n%s\n\nNUMBERED TRANSCRIPT:\n%s", cleanDescription(description), numberedTranscript)
}

func BuildRevealUserMessage(description, tastingSummary, numberedTranscript string) string {
	return fmt.Sprintf("DESCRIPTION:\n%s\n\nTASTING SUMMARY:\n%s\n\nNUMBERED TRANSCRIPT:\n%s", cleanDescription(description), tastingSummary, numberedTranscript)
}

func cleanDescription(desc string) string {
	startKeywords := []string{
		"tasted the following wines",
		"wines in this video",
		"wines in this Video",
		"tasted the following",
	}

	startIndex := -1
	for _, kw := range startKeywords {
		if idx := strings.Index(strings.ToLower(desc), kw); idx != -1 {
			startIndex = idx
			break
		}
	}

	if startIndex == -1 {
		return desc
	}

	cleaned := desc[startIndex:]

	endKeywords := []string{
		"100 point scoring system",
		"100 point scoring",
		"the 100 point",
		"robertparker.com",
	}

	endIndex := -1
	for _, kw := range endKeywords {
		if idx := strings.Index(strings.ToLower(cleaned), kw); idx != -1 {
			endIndex = idx
			break
		}
	}

	if endIndex != -1 {
		cleaned = cleaned[:endIndex]
	}

	return strings.TrimSpace(cleaned)
}

func BuildExtractUserMessage(wineName, focusedText string) string {
	return fmt.Sprintf("WINE NAME:\n%s\n\nTRANSCRIPT SEGMENT:\n%s", wineName, focusedText)
}
