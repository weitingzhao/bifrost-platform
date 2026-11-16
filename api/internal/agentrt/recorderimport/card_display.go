package recorderimport

import (
	"fmt"
	"slices"
	"strings"
)

// Card text only; caller retains validation, approval and writes.
func CardBody(actor, cardKey, reason string, log Log, logID, stream string) string {
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
	fmt.Fprintf(&b, "%s proposes to import the recorder log of %s, proposal %s.\n\nWhy: %s\n\n", actor, s.WorkID, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d events: %s. The hash chain holds from the first line to the last.", len(log.Records), strings.Join(counts, ", "))
	if n := log.Masked(); n > 0 {
		fmt.Fprintf(&b, " %d of them had a path into one machine, masked before they are kept; each keeps the digest of its original line.", n)
	}
	fmt.Fprintf(&b, "\n\nFrom %s, sha256 %s; kept log %s.\n\n", s.File, s.SHA256, logID)
	b.WriteString("Importing writes the log as the stream " + stream + ", one event per line, under your decision; " +
		"it is then fixed. Not importing writes nothing; the recorder keeps its log as it is.")
	return b.String()
}
