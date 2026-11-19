package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The rollup of elastic nodes and the probes of their hosts are tested in
// package computenodes since 2026-11-19, through Summary and Nodes with
// compute's nodes bound.

func TestSummaryReachFromNodes(t *testing.T) {
	reach, detail := summaryReachFromNodes(5, 5, 0, 2)
	if reach != probe.ReachDegraded {
		t.Fatalf("reach: got %s want degraded", reach)
	}
	if detail == "" {
		t.Fatal("expected detail")
	}

	reach, detail = summaryReachFromNodes(5, 5, 0, 0)
	if reach != probe.ReachOK {
		t.Fatalf("reach: got %s want ok", reach)
	}
	detail = appendElasticStandbyDetail(detail, 1)
	if detail != "1 elastic standby" {
		t.Fatalf("detail: got %q", detail)
	}
}
