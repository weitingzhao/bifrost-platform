package artifact_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A refused report is recorded once per body on its host's stream as
// artifact_refused; a second refusal of the same body records nothing; an
// unknown rule or kind, or no host, records nothing; and a body recorded
// before 2026-11-17 as a policy_alert is not recorded again.
//
// Origin: 2026-11-17, migration 0030: the refusal left policy_alert for a type
// of its own; until then nothing tested its recording (TD-382's writer had no
// test).
func TestRecordRefusalOncePerBody(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	count := func(stream, typ string) int {
		t.Helper()
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = $2`, stream, typ).Scan(&n))
		return n
	}
	body := w.artifact(t, "report", "a report that named /Users/someone/x")
	for range 2 {
		recorded, err := artifact.RecordRefusal(ctx, w.rt, "h1", "report", body, "local_path")
		ok(t, err)
		if !recorded {
			t.Fatal("a refusal with a host, a known kind and rule was not taken")
		}
	}
	if n := count("host:h1", "artifact_refused"); n != 1 {
		t.Fatalf("%d artifact_refused events for one body; want 1", n)
	}
	var rule, kind string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'rule', body_json->>'kind' FROM agentrt.event WHERE event_type = 'artifact_refused'`).Scan(&rule, &kind))
	if rule != "local_path" || kind != "report" {
		t.Fatalf("recorded rule %q kind %q", rule, kind)
	}
	for _, c := range []struct{ host, kind, rule string }{{"", "report", "local_path"}, {"h1", "spec", "local_path"}, {"h1", "report", "secret"}} {
		if recorded, err := artifact.RecordRefusal(ctx, w.rt, c.host, c.kind, body, c.rule); err != nil || recorded {
			t.Errorf("%+v: recorded %v, %v", c, recorded, err)
		}
	}
	// A body recorded the old way stays recorded once.
	old := w.artifact(t, "verify", "a verification report that named another workspace")
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "host:h2", Type: "policy_alert", Actor: "controller",
			Body: map[string]any{"rule": "workspace_ref", "source": "verify", "phrase": "another task's workspace", "artifact_id": old}})
		return err
	}))
	if _, err := artifact.RecordRefusal(ctx, w.rt, "h2", "verify", old, "workspace_ref"); err != nil {
		t.Fatal(err)
	}
	if n := count("host:h2", "artifact_refused"); n != 0 {
		t.Fatalf("a body recorded as a policy_alert was recorded again (%d)", n)
	}
}

// The refusal's type is registered in bus/registry/ledger/types.yaml under
// foundation.
//
// Origin: 2026-11-17, migration 0030.
func TestRefusalTypeIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "bus", "registry", "ledger", "types.yaml"))
	ok(t, err)
	var doc struct {
		Types []struct{ Type, Domain string } `yaml:"types"`
	}
	ok(t, yaml.Unmarshal(raw, &doc))
	for _, lt := range doc.Types {
		if lt.Type == "artifact_refused" {
			if lt.Domain != "foundation" {
				t.Fatalf("artifact_refused is registered under %q", lt.Domain)
			}
			return
		}
	}
	t.Fatalf("artifact_refused is not registered (%d types read)", len(doc.Types))
}
