package actions

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The exported id list is the cross-language ratchet for MCP write mappings.
func TestExportedActionsCatalogMatchesCatalog(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "config", "actions-catalog.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc struct {
		Actions []string `json:"actions"`
	}
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("trailing JSON: %v", err)
	}
	got := Catalog()
	if len(doc.Actions) != len(got) {
		t.Fatalf("actions-catalog.json has %d ids, Catalog() has %d", len(doc.Actions), len(got))
	}
	for i, action := range got {
		if doc.Actions[i] != action.ID {
			t.Fatalf("actions[%d] = %q, Catalog()[%d] = %q", i, doc.Actions[i], i, action.ID)
		}
	}
}
