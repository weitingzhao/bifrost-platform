//bifrost:owner lineage

package store

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

func original_recorderCardBody(w Writer, cardKey, reason string, log recorderimport.Log, logID string) string {
	types := log.Types()
	names := make([]string, 0, len(types))
	for t := range types {
		names = append(names, t)
	}
	slices.Sort(names)
	var counts []string
	for _, t := range names {
		counts = append(counts, fmt.Sprintf("%d %s", types[t], t))
	}
	s := log.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the recorder log of %s, proposal %s.\n\nWhy: %s\n\n", w.Actor, s.WorkID, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d events: %s. The hash chain holds from the first line to the last.", len(log.Records), strings.Join(counts, ", "))
	if n := log.Masked(); n > 0 {
		fmt.Fprintf(&b, " %d of them had a path into one machine, masked before they are kept; each keeps the digest of its original line.", n)
	}
	fmt.Fprintf(&b, "\n\nFrom %s, sha256 %s; kept log %s.\n\n", s.File, s.SHA256, logID)
	b.WriteString("Importing writes the log as the stream " + RecorderStream(s.WorkID) + ", one event per line, under your decision; " +
		"it is then fixed. Not importing writes nothing; the recorder keeps its log as it is.")
	return b.String()
}

// The recorder import's card, held to its text before the card texts moved
// to their domains' packages. It was the third part of
// TestCardDisplaysFrozenParity (card_display_baseline_test.go), which keeps
// the plan's two cards.
func TestRecorderCardDisplayFrozenParity(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"", "why\nsecond", "<>&中文"} {
		for _, records := range [][]recorderimport.Record{nil, {{Type: "z"}, {Type: "a", Masked: true}, {Type: "z"}}} {
			log := recorderimport.Log{Source: recorderimport.Source{WorkID: "W-1", File: "log", SHA256: "hash"}, Records: records}
			w := Writer{Actor: "actor"}
			if got, want := recorderCardBody(w, "0123456789abcdef", reason, log, "id"), original_recorderCardBody(w, "0123456789abcdef", reason, log, "id"); got != want {
				t.Fatalf("recorder differs: %q / %q", got, want)
			}
		}
	}
}
