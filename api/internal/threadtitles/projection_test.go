package threadtitles

import (
	"reflect"
	"testing"
)

func TestTranscriptNamesKeepsKeySpacesSeparate(t *testing.T) {
	t.Parallel()
	source := Titles{Transcripts: map[string]Entry{"t1": {Title: "Synced"}, "t2": {Title: ""}}, Manual: map[string]Entry{"t1": {Title: "Manual"}, "session": {Title: "Session"}}}
	got := TranscriptNames(source)
	if !reflect.DeepEqual(got, map[string]string{"t1": "Synced", "t2": ""}) {
		t.Fatalf("title projection changed: %v", got)
	}
	got["t1"] = "edited"
	if source.Transcripts["t1"].Title != "Synced" {
		t.Fatal("output aliases source")
	}
	if empty := TranscriptNames(Titles{}); empty == nil || len(empty) != 0 {
		t.Fatalf("empty map became null: %v", empty)
	}
}
