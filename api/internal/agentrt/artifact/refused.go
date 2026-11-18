package artifact

import (
	"context"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A report agentd uploads that the platform refuses (TD-382) is recorded on
// its host's stream: which rule refused it (local_path: a path into one
// machine, H8; workspace_ref: another task's workspace), what kind of report
// it was, and the body's id. The event is this package's own type,
// artifact_refused (registered under foundation; migration 0030). Until
// 2026-11-17 it was a policy_alert written by the store's RefusedReport, with
// the rule's phrase; those rows stay, and a body recorded then is not
// recorded again.

// refusedType is the event type of a refused report.
const refusedType = "artifact_refused"

// refusedRules are the rules by which the platform refuses an artifact.
var refusedRules = []string{"local_path", "workspace_ref"}

// reportKinds are the kinds a result's report may be stored as.
var reportKinds = []string{"report", "verify", "review"}

// putType is the event that records a stored artifact.
const putType = "artifact_put"

func init() {
	kinds := make([]string, 0, len(ArtifactKinds))
	for k := range ArtifactKinds {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{refusedType, putType},
		Bodies: map[string][]eventlog.Field{
			putType: {{Name: "artifact_id", Kind: eventlog.ID}, {Name: "artifact_kind", Kind: eventlog.Str},
				{Name: "size_bytes", Kind: eventlog.Int}, {Name: "uploaded_by", Kind: eventlog.Str}},
			refusedType: {{Name: "rule", Kind: eventlog.Str}, {Name: "kind", Kind: eventlog.Str}, {Name: "artifact_id", Kind: eventlog.ID}},
		},
		Values: map[string]map[string]eventlog.Value{
			refusedType: {"rule": eventlog.OneOf(refusedRules...), "kind": eventlog.OneOf(reportKinds...)},
			putType: {"artifact_id": eventlog.Shaped("an artifact id (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)),
				"artifact_kind": eventlog.OneOf(kinds...)},
		},
	})
}

// RecordRefusal records an artifact refusal once per body and host: false
// when there is nothing to record (an unknown rule or kind, no host).
func RecordRefusal(ctx context.Context, db eventlog.DB, hostID, kind, artifactID, rule string) (bool, error) {
	if !contains(reportKinds, kind) || !contains(refusedRules, rule) || hostID == "" {
		return false, nil
	}
	stream := "host:" + hostID
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		// Held to the commit: the same body refused twice at once is
		// recorded once.
		if err := eventlog.LockStream(ctx, tx, stream); err != nil {
			return err
		}
		for _, typ := range []string{refusedType, "policy_alert"} {
			if seen, err := eventlog.Has(ctx, tx, stream, typ, map[string]string{"artifact_id": artifactID}); err != nil || seen {
				return err
			}
		}
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: refusedType, Actor: "controller",
			Body: map[string]any{"rule": rule, "kind": kind, "artifact_id": artifactID}})
		return err
	})
	return err == nil, err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
