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
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
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
	host, err := hostField(obj)
	if err != nil {
		return nil, "", err
	}
	redacted, ok := redactValue(obj).(map[string]any)
	if !ok {
		return nil, "", errNotObject
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		return nil, "", fmt.Errorf("document: %w", err)
	}
	return raw, host, nil
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

func redactValue(v any) any {
	switch t := v.(type) {
	case string:
		return approvalnotify.Redact(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[approvalnotify.Redact(k)] = redactValue(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redactValue(child)
		}
		return out
	default:
		return v
	}
}
