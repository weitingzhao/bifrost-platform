package controller_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-401 (STG 2026-10-21 M-79): the pass lists the open cards, reads each
// one's evidence, then gives its assessment. A card answered while its
// evidence is read (the rule answers within seconds on STG) is read again
// through the question store and given no assessment; one still open is.
func TestTheAssessmentsPassReadsACardAgain(t *testing.T) {
	pool := setup(t)
	open := store.Question{Status: "open"}
	w := &world{questions: map[string]store.Question{"q_9": open, "q_10": open}}
	src := w.sources()
	card := func(id string) store.AssessItem {
		return store.AssessItem{Kind: store.AssessCard, ID: id, Title: "Plan change to R2", Body: "retitle a started step",
			Options: []store.AssessOption{{Key: "a", Label: "Apply the change"}, {Key: "b", Label: "Do not apply it"}}, CreatedAt: time.Now()}
	}
	src.Assessable = func(context.Context) ([]store.AssessItem, error) {
		return []store.AssessItem{card("q_9"), card("q_10")}, nil
	}
	src.Evidence = func(context.Context, store.EvidenceQuery) store.Evidence {
		w.set(func() {
			w.questions["q_9"] = store.Question{ID: "q_9", Status: store.QuestionAnswered, Option: "b", AnsweredBy: "rule card-auto-answer v3",
				AnsweredAt: time.Now().UTC().Format(time.RFC3339), Channel: "rule"}
		})
		return store.Evidence{}
	}
	for _, id := range []string{"q_9", "q_10"} {
		must(t, store.RequestAssessment(ctx, pool, store.Writer{Actor: "owner"}, store.AssessCard, id, func(context.Context) error { return nil }))
	}
	c := &controller.Controller{DB: pool, Sources: src}
	if n, err := c.Assess(ctx); err != nil || n != 1 {
		t.Fatalf("asked %d (%v); want only the card still open", n, err)
	}
	for id, want := range map[string]int{"q_9": 0, "q_10": 1} {
		var missions int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.mission WHERE scope_json->>'assessment_of' = $1`,
			store.AssessStream(store.AssessCard, id)).Scan(&missions))
		if missions != want {
			t.Errorf("%s: %d missions assess it; want %d", id, missions, want)
		}
	}
}

// TD-463: opening an item never starts its evidence reads or paid task.
func TestAssessmentPassWaitsForOwnerRequest(t *testing.T) {
	pool := setup(t)
	reads := 0
	items := []store.AssessItem{
		{Kind: store.AssessApproval, ID: "463", Title: "Approve a change", Options: store.ApprovalOptions(), CreatedAt: time.Now()},
		{Kind: store.AssessCard, ID: "q_463", Title: "Choose", Options: store.ApprovalOptions(), CreatedAt: time.Now()},
	}
	c := &controller.Controller{DB: pool, Sources: controller.Sources{
		Assessable: func(context.Context) ([]store.AssessItem, error) { return items, nil },
		Evidence:   func(context.Context, store.EvidenceQuery) store.Evidence { reads++; return store.Evidence{} },
	}}
	if n, err := c.Assess(ctx); err != nil || n != 0 || reads != 0 {
		t.Fatalf("unrequested: %d tasks, %d reads, %v", n, reads, err)
	}
	for _, item := range items {
		must(t, store.RequestAssessment(ctx, pool, store.Writer{Actor: "owner"}, item.Kind, item.ID, func(context.Context) error { return nil }))
	}
	if n, err := c.Assess(ctx); err != nil || n != 2 || reads != 2 {
		t.Fatalf("requested: %d tasks, %d reads, %v", n, reads, err)
	}
	if n, err := c.Assess(ctx); err != nil || n != 0 || reads != 2 {
		t.Fatalf("repeated: %d tasks, %d reads, %v", n, reads, err)
	}
}

func TestRequestedApprovalClosesDuringEvidenceRead(t *testing.T) {
	pool := setup(t)
	item := store.AssessItem{Kind: store.AssessApproval, ID: "464", Title: "A pending request", Options: store.ApprovalOptions(), CreatedAt: time.Now()}
	must(t, store.RequestAssessment(ctx, pool, store.Writer{Actor: "owner"}, item.Kind, item.ID, func(context.Context) error { return nil }))
	closed := false
	c := &controller.Controller{DB: pool, Sources: controller.Sources{
		Assessable: func(context.Context) ([]store.AssessItem, error) { return []store.AssessItem{item}, nil },
		Evidence:   func(context.Context, store.EvidenceQuery) store.Evidence { closed = true; return store.Evidence{} },
		CheckAssessment: func(context.Context, string, string) error {
			if closed {
				return fmt.Errorf("%w: approval is rejected", store.ErrState)
			}
			return nil
		},
	}}
	if n, err := c.Assess(ctx); err != nil || n != 0 {
		t.Fatalf("closed approval: %d tasks, %v", n, err)
	}
	raw, err := store.ReadAssessment(ctx, pool, item.Kind, item.ID)
	must(t, err)
	if !strings.Contains(string(raw), "approval is rejected") {
		t.Fatalf("no unavailability reason: %s", raw)
	}
}
