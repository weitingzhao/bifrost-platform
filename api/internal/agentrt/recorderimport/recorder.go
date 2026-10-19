// Package recorderimport reads a log of the infra event recorder
// (agent-config/scripts/recorder, events.py: each line the sha256 of its
// body, a space, and the body, a canonical JSON object whose prev_sha256 is
// the line before's digest) into the form the runtime keeps and imports as
// the stream legacy:recorder:<work> (v5 §5.3 T32 「经导入」, the recorder's
// half; CUT-gate-b-2026-10-19.md §2 item 3 A, Owner 2026-10-19 「都照建议」).
// It reads and writes nothing but its arguments.
//
// The chain is checked on the lines as the recorder wrote them. A line with a
// path into one machine (rule H8) is then masked (localpath.Mask), so the
// kept log carries no local path; its digest is the original's, so a masked
// record's body no longer hashes to it, and Check says so instead of
// pretending: it holds the links of every record and the digest of every
// record not masked.
package recorderimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// Format names the request's shape and the kept log's.
const Format = "agentrt.recorder_import/v1"

// Bounds: the recorder's own (events.py MAX_EVENTS) and a line's.
const (
	MaxLines     = 20000
	maxLineBytes = 64 << 10
)

// Request is one recorder log as read: its lines, each without its newline.
type Request struct {
	Format string   `json:"format"`
	Source Source   `json:"source"`
	Lines  []string `json:"lines"`
}

// Source says which log the lines are.
type Source struct {
	WorkID string `json:"work_id"` // the recorder's work: the log's directory
	File   string `json:"file"`    // as the reader named it, relative: "W-31/events.jsonl"
	SHA256 string `json:"sha256"`  // of the lines, each followed by a newline
}

// Record is one event of a kept log: the original digest, and the body as
// the recorder wrote it or, when Masked, with its local paths masked.
type Record struct {
	Digest string `json:"digest"`
	Masked bool   `json:"masked,omitempty"`
	Body   string `json:"body"`
	Seq    int    `json:"-"`
	Type   string `json:"-"`
}

// Log is a request read and checked: what the runtime keeps and imports.
type Log struct {
	Format  string   `json:"format"`
	Source  Source   `json:"source"`
	Records []Record `json:"records"`
}

var (
	workID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	hex64  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	file   = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Read checks a request (its source, every line's digest, the chain from the
// first line: v 2, seq 1, 2, … and each prev_sha256 the digest before, the
// first ""), masks the local paths of each record, and gives the log to
// keep. The error says which line breaks which rule.
func Read(req Request) (Log, error) {
	if req.Format != Format {
		return Log{}, fmt.Errorf("format %q is not %s", req.Format, Format)
	}
	if err := req.Source.check(); err != nil {
		return Log{}, err
	}
	switch n := len(req.Lines); {
	case n == 0:
		return Log{}, errors.New("a log has at least one line")
	case n > MaxLines:
		return Log{}, fmt.Errorf("a log has at most %d lines, not %d", MaxLines, n)
	}
	var all bytes.Buffer
	for _, l := range req.Lines {
		all.WriteString(l)
		all.WriteByte('\n')
	}
	if got := sum(all.Bytes()); got != req.Source.SHA256 {
		return Log{}, fmt.Errorf("source.sha256 is %s; the lines hash to %s", req.Source.SHA256, got)
	}
	out := Log{Format: Format, Source: req.Source, Records: make([]Record, 0, len(req.Lines))}
	prev := ""
	for i, l := range req.Lines {
		r, err := readLine(l, i+1, prev, req.Source.WorkID)
		if err != nil {
			return Log{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		if r, err = mask(r); err != nil {
			return Log{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		out.Records = append(out.Records, r)
		prev = r.Digest
	}
	return out, nil
}

func (s Source) check() error {
	switch {
	case !workID.MatchString(s.WorkID):
		return fmt.Errorf("source.work_id %q is not a work id", s.WorkID)
	case !file.MatchString(s.File) || strings.Contains("/"+s.File+"/", "/../") || strings.Contains("/"+s.File+"/", "/./"):
		return fmt.Errorf("source.file %q is not a relative path", s.File)
	case !hex64.MatchString(s.SHA256):
		return errors.New("source.sha256 is not 64 lowercase hex digits")
	}
	return nil
}

// head is what a body says of its place in the chain.
type head struct {
	V      *int    `json:"v"`
	Seq    *int    `json:"seq"`
	Prev   *string `json:"prev_sha256"`
	Type   *string `json:"type"`
	WorkID *string `json:"work_id"`
}

// readLine checks one line as the recorder sealed it.
func readLine(l string, seq int, prev, work string) (Record, error) {
	if len(l) > maxLineBytes {
		return Record{}, fmt.Errorf("longer than %d bytes", maxLineBytes)
	}
	digest, body, found := strings.Cut(l, " ")
	switch {
	case !found || !hex64.MatchString(digest):
		return Record{}, errors.New("not a digest, a space and a body")
	case sum([]byte(body)) != digest:
		return Record{}, errors.New("the body does not hash to its digest")
	}
	h, err := readHead(body)
	if err != nil {
		return Record{}, err
	}
	switch {
	case *h.V != 2:
		return Record{}, fmt.Errorf("v is %d, not 2", *h.V)
	case *h.Seq != seq:
		return Record{}, fmt.Errorf("seq is %d, not %d", *h.Seq, seq)
	case *h.Prev != prev:
		return Record{}, errors.New("prev_sha256 is not the digest of the line before")
	case h.WorkID != nil && *h.WorkID != work:
		return Record{}, fmt.Errorf("work_id %q is not the log's %q", *h.WorkID, work)
	}
	return Record{Digest: digest, Body: body, Seq: seq, Type: *h.Type}, nil
}

// readHead reads a body: one JSON object, no key twice, with v, seq,
// prev_sha256 and a type.
func readHead(body string) (head, error) {
	var h head
	if !json.Valid([]byte(body)) {
		return h, errors.New("the body is not JSON")
	}
	if err := noDuplicateKeys([]byte(body)); err != nil {
		return h, err
	}
	dec := json.NewDecoder(strings.NewReader(body))
	if err := dec.Decode(&h); err != nil {
		return h, fmt.Errorf("the body is not a recorder event: %v", err)
	}
	switch {
	case h.V == nil || h.Seq == nil || h.Prev == nil:
		return h, errors.New("the body has no v, seq or prev_sha256")
	case h.Type == nil || *h.Type == "":
		return h, errors.New("the body has no type")
	}
	return h, nil
}

// noDuplicateKeys refuses a document with a key twice in one object: Go and
// Python would read different values from it.
func noDuplicateKeys(doc []byte) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := k.(string)
				if seen[key] {
					return fmt.Errorf("the key %q is given twice", key)
				}
				seen[key] = true
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	return walk()
}

// mask masks the local paths of a record's body, which must stay one valid
// JSON object with no local path left and name no task's workspace.
func mask(r Record) (Record, error) {
	if m := localpath.Mask(r.Body); m != r.Body {
		masked, ok := localpath.MaskJSON([]byte(m))
		if !ok {
			return r, errors.New("its local paths cannot be masked into valid JSON")
		}
		r.Body, r.Masked = string(masked), true
	}
	if _, found := localpath.Find([]byte(r.Body)); found {
		return r, errors.New("a local path is left after masking")
	}
	if ref, found := localpath.WorkspaceRef([]byte(r.Body)); found {
		return r, fmt.Errorf("it names a task's workspace: %s", ref)
	}
	return r, nil
}

// Check checks a kept log as it is read back: its format and source, the
// chain of every record (seq, prev_sha256), the digest of every record not
// masked, and that a masked one is still a recorder event with no local path.
func Check(l Log) error {
	if l.Format != Format {
		return fmt.Errorf("format %q is not %s", l.Format, Format)
	}
	if err := l.Source.check(); err != nil {
		return err
	}
	if n := len(l.Records); n == 0 || n > MaxLines {
		return fmt.Errorf("a log has 1 to %d records, not %d", MaxLines, n)
	}
	prev := ""
	for i := range l.Records {
		r := &l.Records[i]
		if !hex64.MatchString(r.Digest) {
			return fmt.Errorf("record %d: the digest is not 64 lowercase hex digits", i+1)
		}
		if !r.Masked && sum([]byte(r.Body)) != r.Digest {
			return fmt.Errorf("record %d: the body does not hash to its digest", i+1)
		}
		h, err := readHead(r.Body)
		switch {
		case err != nil:
			return fmt.Errorf("record %d: %w", i+1, err)
		case *h.V != 2 || *h.Seq != i+1 || *h.Prev != prev:
			return fmt.Errorf("record %d: the chain breaks here", i+1)
		case h.WorkID != nil && *h.WorkID != l.Source.WorkID:
			return fmt.Errorf("record %d: work_id %q is not the log's", i+1, *h.WorkID)
		}
		if again, err := mask(*r); err != nil || again.Body != r.Body {
			return fmt.Errorf("record %d: a local path or a workspace is left", i+1)
		}
		r.Seq, r.Type = i+1, *h.Type
		prev = r.Digest
	}
	return nil
}

// Masked counts the records whose local paths were masked.
func (l Log) Masked() int {
	n := 0
	for _, r := range l.Records {
		if r.Masked {
			n++
		}
	}
	return n
}

// Types counts the records by event type.
func (l Log) Types() map[string]int {
	out := map[string]int{}
	for _, r := range l.Records {
		out[r.Type]++
	}
	return out
}

// FromLog makes the request for a work's log as the recorder left it on
// disk: its complete lines (bytes after the last newline are a torn tail and
// not part of the log, as events.py reads it), named "<work>/events.jsonl".
func FromLog(work string, data []byte) Request {
	if cut := bytes.LastIndexByte(data, '\n'); cut >= 0 {
		data = data[:cut+1]
	} else {
		data = nil
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
	return Request{Format: Format, Lines: lines, Source: Source{WorkID: work, File: work + "/events.jsonl", SHA256: sum(data)}}
}
