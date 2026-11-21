// Command agentrt-recorder-import makes the request that imports one work's
// log of the infra event recorder (DIR/ID/events.jsonl; format
// agentrt.recorder_import/v1, CUT-gate-b-2026-10-19.md §2 item 3 A) and checks
// it as the runtime will: the hash chain, and the local paths it will mask.
// It reads and writes files only: the request goes to the platform through the
// MCP tool propose_plan_import.
//
//	agentrt-recorder-import -root DIR -work ID [-o request.json]
//
// It was the recorder subcommand of agentrt-plan-import until 2026-11-21; the
// recorder log is the lineage domain's, and that command is the plan domain's,
// so the plan command's import of recorderimport was a line on the domain
// wall's baseline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

func main() {
	os.Exit(recorder(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func sayf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

// recorder writes the request for one work's recorder log, after checking
// it as the runtime will.
func recorder(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recorder", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "the recorder's root directory")
	work := fs.String("work", "", "the work whose log to import (a directory under -root)")
	out := fs.String("o", "", "write the request here instead of to standard output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *root == "" || *work == "" {
		say(stderr, "-root and -work are required")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *work, "events.jsonl"))
	if err != nil {
		say(stderr, err)
		return 1
	}
	req := recorderimport.FromLog(*work, data)
	log, err := recorderimport.Read(req)
	if err != nil {
		sayf(stderr, "%s: %v\n", *work, err)
		return 1
	}
	sayf(stderr, "%s: %d events, chain holds, %d with local paths to mask\n", *work, len(log.Records), log.Masked())
	doc, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		say(stderr, err)
		return 1
	}
	doc = append(doc, '\n')
	if why := recorderFits(req, log, doc); why != "" {
		sayf(stderr, "%s: %s; nothing written\n", *work, why)
		return 1
	}
	if *out == "" {
		_, err = stdout.Write(doc)
	} else {
		err = os.WriteFile(*out, doc, 0o644)
	}
	if err != nil {
		say(stderr, err)
		return 1
	}
	return 0
}

// importBytes bounds what one recorder import may be, at each layer it
// passes (review 489 B2): the request file the MCP tool reads
// (PLAN_IMPORT_MAX_BYTES), the JSON body POST /plan/imports takes (its
// maxBody), and the log the store keeps as one artifact (maxArtifactBytes);
// each is 1 MiB.
const importBytes = 1 << 20

// envelopeRoom is the room a request leaves for its body's idempotency key
// and reason: a reservation, not their bound (review 491 B2) — the MCP tool
// checks the body it sends, with the key and reason it is given, against
// the route's 1 MiB.
const envelopeRoom = 16 << 10

// recorderFits says which layer a request (written as doc) would not pass,
// "" when it passes every one: a log too long for them needs to be split by
// design, not by hand (a later part's chain does not start at seq 1, and a
// work is imported once).
func recorderFits(req recorderimport.Request, log recorderimport.Log, doc []byte) string {
	if len(doc) > importBytes {
		return fmt.Sprintf("the request is %d bytes, over the %d the MCP tool reads", len(doc), importBytes)
	}
	compact, err := json.Marshal(req)
	if err != nil {
		return err.Error()
	}
	if len(compact)+envelopeRoom > importBytes {
		return fmt.Sprintf("the request sent is %d bytes with its envelope, over the %d POST /plan/imports takes", len(compact)+envelopeRoom, importBytes)
	}
	raw, err := json.Marshal(log)
	if err != nil {
		return err.Error()
	}
	kept, err := canon.Marshal(raw)
	if err != nil {
		return err.Error()
	}
	if len(kept) > importBytes {
		return fmt.Sprintf("the log kept is %d bytes, over the %d an artifact is", len(kept), importBytes)
	}
	return ""
}
