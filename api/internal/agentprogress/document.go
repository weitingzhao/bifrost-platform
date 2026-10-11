// Package agentprogress keeps the latest coordination progress document.
//
// This is an interim bridge (DESIGN §20.4): the coordinating thread publishes
// the document its local board already computes; the platform keeps the latest
// one; the Console draws it. Later the runtime computes it from events.
package agentprogress

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

const (
	// MaxDocumentBytes is the largest accepted PUT body (256 KiB).
	MaxDocumentBytes int64 = 256 << 10
	// StaleAfter is how old a document may be before readers are told it is
	// not current. Older than this, not equal to it.
	StaleAfter = 5 * time.Minute
	// StateRel is the state file under PLATFORM_DATA_DIR. In the cluster that
	// path is a ConfigMap key through statefile.
	StateRel = "agent-progress/state.json"
)

// Same host rule as a thread heartbeat host.
var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	errNotObject = errors.New("document: want a JSON object with a numeric at")
	errHost      = errors.New("host: want a host name")
	// errTooLarge is a snapshot that would not fit the state file. PUT answers
	// 413 and keeps the previous document.
	errTooLarge = errors.New("document too large")
)

// Snapshot is the one document the platform keeps.
type Snapshot struct {
	Document   json.RawMessage `json:"document"`
	ReceivedAt time.Time       `json:"received_at"`
	Host       string          `json:"host,omitempty"`
}

// prepare checks the body, redacts every string, and returns the document to
// store plus the reporter host. Unknown fields are kept.
func prepare(body []byte) (json.RawMessage, string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, "", errNotObject
	}
	var trail json.RawMessage
	if err := dec.Decode(&trail); !errors.Is(err, io.EOF) {
		return nil, "", errNotObject
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, "", errNotObject
	}
	num, ok := obj["at"].(json.Number)
	if !ok {
		return nil, "", errNotObject
	}
	if _, err := num.Float64(); err != nil {
		return nil, "", errNotObject
	}
	if err := validateDocument(obj); err != nil {
		return nil, "", err
	}
	host, err := hostField(obj)
	if err != nil {
		return nil, "", err
	}
	redacted, ok := redactValue(obj).(map[string]any)
	if !ok {
		return nil, "", errNotObject
	}
	// encoding/json HTML-escapes <, > and & to six bytes each. A body under the
	// input cap can become a snapshot the state file will never accept. Encode
	// the bytes we store, and refuse them before they are kept.
	raw, err := marshalJSON(redacted)
	if err != nil {
		return nil, "", fmt.Errorf("document: %w", err)
	}
	return raw, host, nil
}

// marshalJSON encodes without HTML escaping. json.Encoder adds a trailing
// newline; the stored snapshot does not include it.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func hostField(obj map[string]any) (string, error) {
	v, present := obj["host"]
	if !present || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", errHost
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !hostRe.MatchString(s) {
		return "", errHost
	}
	return s, nil
}

// redactValue and redactNested are the progress-document walk. The rules live in notice.
func redactValue(v any) any { return notice.RedactDocument(v, false) }

func redactNested(v any, force bool) any { return notice.RedactDocument(v, force) }

// generatedTime is the document's at as UTC. ok is false when at is missing
// or not a finite number.
func generatedTime(doc json.RawMessage) (time.Time, bool) {
	var probe struct {
		At *float64 `json:"at"`
	}
	if err := json.Unmarshal(doc, &probe); err != nil || probe.At == nil {
		return time.Time{}, false
	}
	sec := *probe.At
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return time.Time{}, false
	}
	whole, frac := math.Modf(sec)
	if frac < 0 {
		whole--
		frac++
	}
	return time.Unix(int64(whole), int64(math.Round(frac*1e9))).UTC(), true
}
