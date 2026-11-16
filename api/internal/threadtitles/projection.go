package threadtitles

// TranscriptNames projects synced transcript titles for the thread list.
// Manual session titles are a different key space and are not substituted.
func TranscriptNames(t Titles) map[string]string {
	out := make(map[string]string, len(t.Transcripts))
	for id, e := range t.Transcripts {
		out[id] = e.Title
	}
	return out
}
