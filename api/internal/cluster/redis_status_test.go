package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestSummarizeRedisStatusEmbeddedOnly(t *testing.T) {
	resp := RedisStatusResponse{
		TargetsTotal:   5,
		TargetsReady:   0,
		EmbeddedActive: 3,
	}
	reach, summary := summarizeRedisStatus(resp)
	// Queue targets retired — embedded-only is still OK (not domain-degraded).
	if reach != probe.ReachOK {
		t.Fatalf("reach=%s", reach)
	}
	if summary == "" {
		t.Fatal("expected summary")
	}
}

func TestSummarizeRedisStatusAllTargets(t *testing.T) {
	resp := RedisStatusResponse{
		TargetsTotal:   5,
		TargetsReady:   5,
		EmbeddedActive: 0,
	}
	reach, _ := summarizeRedisStatus(resp)
	if reach != probe.ReachOK {
		t.Fatalf("reach=%s", reach)
	}
}
