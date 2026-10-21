package controller_test

import (
	"context"
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
