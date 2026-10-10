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

// Nested keys this binary does not know survive a rewrite of another record
// and a later update of this record that only touches known fields.
func TestStoreKeepsNestedUnknownFields(t *testing.T) {
	const execFlag = `{"z":1, "a":2}`
	const delFlag = `{"q":9, "s":"keep"}`
	raw := []byte(`{"last_number":2,"approvals":[` +
		`{"id":"appr_keep","action":"cordon_node","tier":"C","params":{"name":"a"},"params_hash":"abc","status":"running","reason":"r","requester":"s","created_at":"2026-10-10T12:00:00Z","expires_at":"2026-10-11T12:00:00Z",` +
		`"execution":{"attempts":1,"lease_id":"lease-1","new_flag":` + execFlag + `},` +
		`"deliveries":[{"channel":"ntfy","at":"2026-10-10T12:05:00Z","result":"accepted","new_flag":` + delFlag + `}]},` +
		`{"id":"appr_other","action":"cordon_node","tier":"C","params":{"name":"b"},"params_hash":"def","status":"pending","reason":"r","requester":"s","created_at":"2026-10-10T12:00:00Z","expires_at":"2026-10-11T12:00:00Z"}` +
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
	assertNestedFlags(t, path, execFlag, delFlag, `"running"`, "1", `"accepted"`)

	if err := st.update(func(doc *file) error {
		for i := range doc.Approvals {
			if doc.Approvals[i].ID != "appr_keep" {
				continue
			}
			doc.Approvals[i].Status = StatusRejected
			doc.Approvals[i].RejectReason = "no"
			doc.Approvals[i].Execution.Attempts = 3
			doc.Approvals[i].Deliveries[0].Result = "failed"
			return nil
		}
		t.Fatal("record to update was not loaded")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertNestedFlags(t, path, execFlag, delFlag, `"rejected"`, "3", `"failed"`)
}

func assertNestedFlags(t *testing.T, path, execFlag, delFlag, status, attempts, result string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(top["approvals"], &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(row, &obj); err != nil {
			t.Fatal(err)
		}
		if string(obj["id"]) != `"appr_keep"` {
			continue
		}
		if string(obj["status"]) != status {
			t.Fatalf("status = %s, want %s", obj["status"], status)
		}
		var exec map[string]json.RawMessage
		if err := json.Unmarshal(obj["execution"], &exec); err != nil {
			t.Fatal(err)
		}
		if string(exec["new_flag"]) != execFlag {
			t.Fatalf("execution.new_flag = %s, want %s", exec["new_flag"], execFlag)
		}
		if string(exec["attempts"]) != attempts {
			t.Fatalf("attempts = %s, want %s", exec["attempts"], attempts)
		}
		var dels []json.RawMessage
		if err := json.Unmarshal(obj["deliveries"], &dels); err != nil {
			t.Fatal(err)
		}
		var del map[string]json.RawMessage
		if err := json.Unmarshal(dels[0], &del); err != nil {
			t.Fatal(err)
		}
		if string(del["new_flag"]) != delFlag {
			t.Fatalf("deliveries[0].new_flag = %s, want %s", del["new_flag"], delFlag)
		}
		if string(del["result"]) != result {
			t.Fatalf("delivery result = %s, want %s", del["result"], result)
		}
		return
	}
	t.Fatalf("appr_keep missing from %s", got)
}
