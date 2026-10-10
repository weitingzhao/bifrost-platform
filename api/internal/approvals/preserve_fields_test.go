package approvals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A document-level field and a field on a record this binary does not know
// survive, byte-for-value, an update of a different record.
func TestStoreKeepsUnknownFieldsByteForValue(t *testing.T) {
	const docExtra = `{"z":1, "a":2}`
	const keepExtra = `{"keep":true,"n":"byte-for-value"}`
	const otherExtra = `{"q":1}`
	raw := []byte(`{"schema_epoch":` + docExtra + `,"last_number":2,"approvals":[` +
		`{"id":"appr_keep","action":"cordon_node","tier":"C","params":{"name":"a"},"params_hash":"abc","status":"pending","reason":"r","requester":"s","created_at":"2026-10-10T12:00:00Z","expires_at":"2026-10-11T12:00:00Z","legacy_flag":` + keepExtra + `},` +
		`{"id":"appr_other","action":"cordon_node","tier":"C","params":{"name":"b"},"params_hash":"def","status":"pending","reason":"r","requester":"s","created_at":"2026-10-10T12:00:00Z","expires_at":"2026-10-11T12:00:00Z","other_flag":` + otherExtra + `}` +
		`]}`)
	path := filepath.Join(t.TempDir(), "approvals")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st := newStore(path)
	if err := st.update(func(doc *file) error {
		for i := range doc.Approvals {
			if doc.Approvals[i].ID == "appr_other" {
				doc.Approvals[i].Status = StatusRejected
				doc.Approvals[i].RejectReason = "no"
				return nil
			}
		}
		t.Fatal("record to update was not loaded")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatal(err)
	}
	if string(top["schema_epoch"]) != docExtra {
		t.Fatalf("document field = %s, want %s", top["schema_epoch"], docExtra)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(top["approvals"], &rows); err != nil {
		t.Fatal(err)
	}
	foundKeep, foundOther := false, false
	for _, row := range rows {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(row, &obj); err != nil {
			t.Fatal(err)
		}
		switch string(obj["id"]) {
		case `"appr_keep"`:
			foundKeep = true
			if string(obj["legacy_flag"]) != keepExtra {
				t.Fatalf("untouched record field = %s, want %s", obj["legacy_flag"], keepExtra)
			}
			if string(obj["status"]) != `"pending"` {
				t.Fatalf("untouched record status = %s", obj["status"])
			}
		case `"appr_other"`:
			foundOther = true
			if string(obj["other_flag"]) != otherExtra {
				t.Fatalf("updated record field = %s, want %s", obj["other_flag"], otherExtra)
			}
			if string(obj["status"]) != `"rejected"` {
				t.Fatalf("updated record status = %s", obj["status"])
			}
		default:
			t.Fatalf("unexpected record %s", obj["id"])
		}
	}
	if !foundKeep || !foundOther {
		t.Fatalf("records keep=%v other=%v body=%s", foundKeep, foundOther, got)
	}
}
