//bifrost:owner verification

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// 2026-11-16: the runs reading was kept as the way back from the evidence
// ledger; after incremental landing it could not stand in for it, so it is
// retired; a CI question with no ledger answers no verdict, never a pass.
//
// Wiring sets the ledger as the delivery service's reader in every
// environment, EVIDENCE_GATE=runs or not: with no ledger connected the
// policy's CI question and the runtime's CI source have no verdict. A server
// with no delivery service wires nothing.
func TestWireEvidenceGateIsTheLedgerEverywhere(t *testing.T) {
	t.Setenv("EVIDENCE_GATE", "runs") // no longer read
	ctx := context.Background()
	commit := strings.Repeat("a", 40)
	for _, env := range []string{"", "dev", "stg", "prod"} {
		s := &Server{delivery: delivery.NewHandlerForTest(nil, nil)}
		s.agentRuntime = &agentRuntime{env: env}
		reader := s.wireEvidenceGate()
		if reader == nil {
			t.Fatalf("env %q: no reader wired", env)
		}
		s.agentRuntime.evidenceCI = reader
		facts := s.delivery.PolicyFacts()
		status, run, err := facts.CIRun(ctx, "bifrost-platform", commit)
		if err == nil || !strings.Contains(err.Error(), "the evidence ledger is not connected") || status != "" || run != "" {
			t.Errorf("env %q: CIRun with no ledger = %q %q %v", env, status, run, err)
		}
		if ok, err := facts.Succeeded(ctx, "bifrost-platform", commit); ok || err == nil {
			t.Errorf("env %q: Succeeded with no ledger = %v %v", env, ok, err)
		}
		if status, run, ok := s.agentRuntimeSources().CI(ctx, "bifrost-platform", commit); ok || status != "" || run != "" {
			t.Errorf("env %q: the runtime read CI with no ledger: %q %q", env, status, run)
		}
	}
	if (&Server{}).wireEvidenceGate() != nil {
		t.Error("a server with no delivery service wired a reader")
	}
}
