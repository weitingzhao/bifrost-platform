package workregistration

import "testing"

// 2026-11-15 domain extraction: preserve the session-binding projection
// while the registered record and its writer stay in their existing format.
func TestIdentityRecordProjection(t *testing.T) {
	raw := []byte(`{"v":1,"kind":"agentrt.external_work/v1","external_work_id":"ew_a","root":"R2","plan_node_id":"R2:1-1","state":"Running","host":"h1","vendor":"codex","session":"s1","epoch":7,"commit_shas":["abc"],"exit_code":0,"future_field":true}`)
	got, err := identityFromJSON(raw)
	want := Identity{ID: "ew_a", Root: "R2", Node: "R2:1-1", State: "Running", Host: "h1", Vendor: "codex", Session: "s1", Epoch: 7}
	if err != nil || got != want {
		t.Fatalf("identity=%+v err=%v want=%+v", got, err, want)
	}
	old, err := identityFromJSON([]byte(`{"external_work_id":"ew_old","vendor":"claude","session":"old"}`))
	if err != nil || old.Host != "" || old.Epoch != 0 || old.ID != "ew_old" {
		t.Fatalf("legacy identity=%+v err=%v", old, err)
	}
}

// 2026-11-15 extraction review: decoding only projected fields would silently
// accept malformed fields the old whole-record decoder rejected. Keep both
// the successful whole-record case above and these negative counterparts.
func TestIdentityStillDecodesOtherRecordFields(t *testing.T) {
	for _, raw := range []string{
		`{"vendor":"codex","commit_shas":7}`,
		`{"vendor":"codex","exit_code":"bad"}`,
		`{"vendor":"codex","seq":"bad"}`,
		`{"vendor":"codex","registered_at":123}`,
		`{"vendor":"codex","epoch":"bad"}`,
	} {
		if _, err := identityFromJSON([]byte(raw)); err == nil {
			t.Errorf("accepted malformed whole record: %s", raw)
		}
	}
}
