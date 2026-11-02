package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/debtimport"
)

func TestParseReportAndExportCLI(t *testing.T) {
	ledger := "../../internal/agentrt/debtimport/testdata/legacy.md"
	ratchets := "../../internal/agentrt/debtimport/testdata/ratchets.md"
	args := []string{"parse", ledger, ratchets, "--at", "2026-11-02T18:00:00Z"}
	var out, report bytes.Buffer
	if err := run(args, &out, &report, time.Now); err != nil {
		t.Fatal(err)
	}
	var r debtimport.Request
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	var review debtimport.Report
	if err := json.Unmarshal(report.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if r.Format != debtimport.Format || review.Counts["filed_at_unavailable"] != 1 {
		t.Fatal("missing request/report")
	}
	var again, againReport bytes.Buffer
	if err := run(args, &again, &againReport, func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), again.Bytes()) || !bytes.Equal(report.Bytes(), againReport.Bytes()) {
		t.Fatal("--at does not fix import context")
	}
	file := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(file, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ which, file string }{{"ledger", ledger}, {"ratchets", ratchets}} {
		var exported bytes.Buffer
		if err := run([]string{"export", file, "--file", tc.which}, &exported, &report, time.Now); err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		expectedLedger, expectedRatchets, err := debtimport.Export(r)
		if err != nil {
			t.Fatal(err)
		}
		expected := expectedLedger
		if tc.which == "ratchets" {
			expected = expectedRatchets
		}
		if !bytes.Equal(exported.Bytes(), expected) {
			t.Fatalf("%s CLI differs from field export", tc.which)
		}
		if tc.which == "ledger" && bytes.Equal(want, expected) {
			t.Fatal("fixture state/gate normalization did not render")
		}
		if review.Counts["rendered_difference"] == 0 {
			t.Fatal("missing normalization report")
		}
	}
}
func TestCLIRejectsAmbiguousInput(t *testing.T) {
	for _, args := range [][]string{nil, {"no-such-command"}, {"parse", "a"}, {"parse", "a", "b", "--at"}, {"parse", "a", "b", "--unknown=x"}, {"parse", "a", "b", "--infra=x", "--infra=y"}, {"export", "a", "--at=2026-11-02T18:00:00Z"}} {
		var out bytes.Buffer
		if err := run(args, &out, &out, time.Now); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	var r debtimport.Request
	if err := decode([]byte(`{"unknown":true}`), &r); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := decode([]byte(`{} {}`), &r); err == nil {
		t.Fatal("multiple values accepted")
	}
	if _, _, err := arguments([]string{"--file=ledger", "x"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := arguments([]string{"--file="}); err == nil || !strings.Contains(err.Error(), "value") {
		t.Fatal("empty flag")
	}
}
