package actionrecord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// An action trace copies the materials the executor already used for one
// action (the request, the decision, the result: the server's actionTrace,
// redacted), on the stream action-trace, for the review of automatic
// decisions afterwards (the Owner keeps their trail seven days). It never
// authorizes anything, and a failed append does not change the action.
//
// Its event type is this package's, action_traced {text} (migration 0032,
// 2026-11-17: one owning domain per event type; it was a note on the same
// stream, whose rows stay).
const tracedType = "action_traced"

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  []string{tracedType},
		Bodies: map[string][]eventlog.Field{tracedType: {{Name: "text", Kind: eventlog.Str}}},
	})
}

// AppendTrace records one action trace, raw being its JSON.
func AppendTrace(ctx context.Context, db eventlog.DB, raw string) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "action-trace", Type: tracedType, Actor: "controller", Body: map[string]any{"text": raw}})
		return err
	})
}

// Trace is the envelope of one action trace: copies of materials the executor
// already used. It never authorizes an action and a failed append must not
// change its result. Approval is the approval the action ran under, when one
// did (the server's approvals.Approval).
type Trace struct {
	Environment string    `json:"environment"`
	Version     int       `json:"version"`
	Action      string    `json:"action"`
	Mode        string    `json:"mode"`
	At          time.Time `json:"at"`
	RequestHash string    `json:"request_hash"`
	Object      any       `json:"object"`
	Initiator   any       `json:"initiator"`
	Reason      string    `json:"reason"`
	Decision    any       `json:"decision"`
	Unchecked   string    `json:"unchecked"`
	Rollback    string    `json:"rollback"`
	Result      any       `json:"result"`
	Approval    any       `json:"approval,omitempty"`
}

// TraceMissing and TraceUnavailable say why a field of a trace is empty: the
// caller did not provide it, or the step has no way to know it.
const (
	TraceMissing     = "not_provided_by_caller"
	TraceUnavailable = "unavailable_at_this_step"
)

// TraceRequiredMessage is the refusal when traces are required and a caller
// left out reason, unchecked or rollback.
const TraceRequiredMessage = "reason, unchecked and rollback are required: reason explains why this action is needed; unchecked lists checks not performed (say none if all were performed); rollback explains how to revert"

// TraceValue is a caller's text under key, or TraceMissing.
func TraceValue(p map[string]any, key string) string {
	v, _ := p[key].(string)
	if strings.TrimSpace(v) == "" {
		return TraceMissing
	}
	return v
}

// NewTrace starts a trace of an action from the parameters it was asked with.
func NewTrace(action, mode string, params map[string]any) *Trace {
	raw, _ := json.Marshal(params)
	hash := sha256.Sum256(raw)
	return &Trace{Version: 1, Action: action, Mode: mode, At: time.Now().UTC(), RequestHash: hex.EncodeToString(hash[:]), Object: params,
		Reason: TraceValue(params, "reason"), Unchecked: TraceValue(params, "unchecked"), Rollback: TraceValue(params, "rollback"), Decision: TraceUnavailable, Result: TraceUnavailable}
}

// Missing says whether the caller left out reason, unchecked or rollback.
func (t *Trace) Missing() bool {
	return t.Reason == TraceMissing || t.Unchecked == TraceMissing || t.Rollback == TraceMissing
}

// RedactTrace redacts before serializing the final envelope, so newlines in
// secret shapes remain visible to the redactor and the resulting JSON stays
// valid. A key whose name makes the redactor hide a value is hidden whole.
func RedactTrace(v any, redact Redactor) any {
	switch x := v.(type) {
	case string:
		return redact(x)
	case map[string]any:
		for k, value := range x {
			if text := redact(k + "=trace-probe"); text != k+"=trace-probe" {
				x[k] = "[redacted]"
			} else {
				x[k] = RedactTrace(value, redact)
			}
		}
	case []any:
		for i := range x {
			x[i] = RedactTrace(x[i], redact)
		}
	}
	return v
}

// Seal stamps the environment and returns the redacted JSON of the trace.
func (t *Trace) Seal(environment string, redact Redactor) (string, error) {
	t.Environment = environment
	raw, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	raw, err = json.Marshal(RedactTrace(value, redact))
	return string(raw), err
}
