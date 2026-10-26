package agentrtapi_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func rejectedDescriptions() map[string][]byte {
	base := string(externalSpec)
	replace := func(old, next string) []byte { return []byte(strings.Replace(base, old, next, 1)) }
	add := func(fields string) []byte { return []byte(base[:len(base)-1] + "," + fields + "}") }
	cases := map[string][]byte{
		"kind_wrong_first":    replace(`"kind":"agentrt.external_work/v1"`, `"kind":"something_else","Kind":"agentrt.external_work/v1"`),
		"kind_wrong_last":     add(`"Kind":"something_else"`),
		"shadow_task":         replace(`"task":"reporter dispatch"`, `"task":"`+strings.Repeat("x", 200000)+`","Task":"valid task"`),
		"shadow_unicode_task": replace(`"task":"reporter dispatch"`, `"task":"`+strings.Repeat("x", 2001)+`","taſk":"valid task"`),
		"shadow_branch":       replace(`"branch":"test"`, `"branch":"`+strings.Repeat("x", 2001)+`","BRANCH":"test"`),
		"duplicate":           add(`"kind":"agentrt.external_work/v1"`),
		"duplicate_escaped":   add(`"\u006bind":"agentrt.external_work/v1"`),
		"unknown":             add(`"payload":{"hidden":"content"}`),
		"trailing":            []byte(base + ` {}`),
		"invalid_utf8":        append([]byte(base), 0xff),
		"wrong_kind":          replace(`agentrt.external_work/v1`, `agentrt.external_work/V1`),
		"wrong_version":       replace(`"v":1`, `"v":2`),
		"float_version":       replace(`"v":1`, `"v":1.0`),
		"null_kind":           replace(`"kind":"agentrt.external_work/v1"`, `"kind":null`),
		"bad_estimate":        replace(`[1,2]`, `[-1,2]`),
		"non_object":          []byte(`[]`),
		"over_one_mib":        []byte(base + strings.Repeat(" ", (1<<20)+1-len(base))),
	}
	// Aliases are invalid even alone, or when their values equal the canonical
	// field. Exercise both orders so last-value-wins cannot hide the bypass.
	for _, field := range []struct{ key, value, alias string }{
		{"v", `1`, "V"}, {"kind", `"agentrt.external_work/v1"`, "Kind"},
		{"task", `"reporter dispatch"`, "Task"}, {"task", `"reporter dispatch"`, "taſk"},
		{"kind", `"agentrt.external_work/v1"`, "Kind"},
		{"executor", `"codex"`, "Executor"}, {"provider", `"openai"`, "PROVIDER"},
		{"repo", `"platform"`, "Repo"}, {"branch", `"test"`, "BRANCH"},
		{"acceptance_refs", `["PV1"]`, "Acceptance_refs"}, {"estimate_minutes", `[1,2]`, "ESTIMATE_MINUTES"},
	} {
		canonical := `"` + field.key + `":` + field.value
		alias := `"` + field.alias + `":` + field.value
		cases[field.alias+"_alone"] = replace(canonical, alias)
		cases[field.alias+"_first"] = replace(canonical, alias+","+canonical)
		cases[field.alias+"_last"] = replace(canonical, canonical+","+alias)
	}
	for _, field := range []struct{ key, value string }{
		{"task", `"reporter dispatch"`}, {"executor", `"codex"`}, {"provider", `"openai"`},
		{"repo", `"platform"`}, {"branch", `"test"`}, {"acceptance_refs", `["PV1"]`},
	} {
		value := `"` + strings.Repeat("x", 2001) + `"`
		if field.key == "acceptance_refs" {
			value = "[" + value + "]"
		}
		cases[field.key+"_oversized"] = replace(`"`+field.key+`":`+field.value, `"`+field.key+`":`+value)
	}
	return cases
}

func TestExternalDescriptionStrictValidation(t *testing.T) {
	for name, raw := range rejectedDescriptions() {
		t.Run(name, func(t *testing.T) {
			if err := store.ValidateExternalDescription(raw); err == nil {
				t.Fatal("accepted invalid description")
			}
		})
	}
}

func TestReporterDescriptionPersistenceBoundary(t *testing.T) {
	pool, _ := world(t)
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+pool.Config().ConnConfig.Database)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	s := server(t, pool)
	counts := func() [3]int {
		t.Helper()
		var n [3]int
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob), (SELECT count(*) FROM agentrt.event)`).Scan(&n[0], &n[1], &n[2]))
		return n
	}
	for name, raw := range rejectedDescriptions() {
		t.Run(name, func(t *testing.T) {
			id := artifactID(raw)
			before := counts()
			want := 403
			if len(raw) > 1<<20 {
				want = 413
			}
			if code, out := call(t, s, "reporter", "PUT", "artifacts/"+id, raw, "X-Artifact-Kind", "other", session, "strict-description"); code != want {
				t.Errorf("upload: want %d, got %d %v", want, code, out)
			}
			if after := counts(); after != before {
				t.Errorf("refused upload changed artifact/blob/event counts: %v -> %v", before, after)
			}
			if len(raw) > 1<<20 {
				return
			}
			// Seed historical bytes directly in this throwaway database: the
			// fixed uploader must not be used to construct an invalid fixture.
			_, err := admin.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
			  VALUES ($1, 'other', 'application/json', $2, 'controller') ON CONFLICT DO NOTHING`, id, len(raw))
			must(t, err)
			_, err = admin.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, raw)
			must(t, err)
			before = counts()
			reg := map[string]any{"idempotency_key": name, "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": name, "artifact_id": id}
			if code, out := call(t, s, "reporter", "POST", "external-work", reg, session, "strict-description"); code != 422 {
				t.Errorf("registration: want 422, got %d %v", code, out)
			}
			if after := counts(); after != before {
				t.Errorf("refused registration changed artifact/blob/event counts: %v -> %v", before, after)
			}
		})
	}
	for name, raw := range map[string][]byte{
		"canonical":  externalSpec,
		"text_limit": []byte(strings.Replace(string(externalSpec), "reporter dispatch", strings.Repeat("é", 1000), 1)),
		"one_mib":    append(append([]byte{}, externalSpec...), bytes.Repeat([]byte(" "), (1<<20)-len(externalSpec))...),
	} {
		t.Run(name, func(t *testing.T) {
			must(t, store.ValidateExternalDescription(raw))
			id := artifactID(raw)
			if code, out := call(t, s, "reporter", "PUT", "artifacts/"+id, raw, "X-Artifact-Kind", "other", session, "valid-description"); code != 200 {
				t.Fatalf("valid upload: %d %v", code, out)
			}
			var stored []byte
			must(t, pool.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id=$1`, id).Scan(&stored))
			if !bytes.Equal(stored, raw) {
				t.Fatal("valid bytes were rewritten")
			}
			reg := map[string]any{"idempotency_key": name, "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": name, "artifact_id": id}
			if code, out := call(t, s, "reporter", "POST", "external-work", reg, session, "valid-description"); code != 200 {
				t.Fatalf("valid registration: %d %v", code, out)
			}
		})
	}
}
