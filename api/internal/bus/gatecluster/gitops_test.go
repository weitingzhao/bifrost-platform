package gatecluster

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

func app(sync, health, rev string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "bifrost-platform-prod", "namespace": "cicd"},
		"status":   map[string]any{"sync": map[string]any{"status": sync, "revision": rev}, "health": map[string]any{"status": health}},
	}}
}

// Origin: 2026-11-18, channel X-A16: whoever waited on an Argo sync polled the
// Application every 2 seconds. The event says each change once, however many
// times the watch and the relist show the same state.
func TestAnApplicationIsSaidOncePerChange(t *testing.T) {
	rev := strings.Repeat("a", 40)
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeApp(ctx, app("Synced", "Healthy", rev))
	g.observeApp(ctx, app("Synced", "Healthy", rev))
	g.observeApp(ctx, app("OutOfSync", "Healthy", rev))
	got := l.events(GitopsStream("bifrost-platform-prod"))
	if len(got) != 2 {
		t.Fatalf("%d events: %+v", len(got), got)
	}
	// A new process remembers nothing and asks the ledger: no repeat.
	g2 := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g2.init()
	g2.observeApp(ctx, app("OutOfSync", "Healthy", rev))
	if n := len(l.events(GitopsStream("bifrost-platform-prod"))); n != 2 {
		t.Fatalf("a restart repeated the state: %d events", n)
	}
	s, ok := AppStateOf(app("Synced", "Progressing", rev))
	if !ok {
		t.Fatal("state not read")
	}
	if err := eventlog.CheckBody(TypeGitopsChanged, gitopsBody(s)); err != nil {
		t.Fatal(err)
	}
	if _, ok := AppStateOf(app("", "", "")); ok {
		t.Fatal("an application without a state was taken")
	}
}
