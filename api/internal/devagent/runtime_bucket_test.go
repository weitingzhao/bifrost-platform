package devagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyRuntimeBucket(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		job          string
		status       string
		allDone      bool
		promptReady  bool
		pending      int
		want         string
	}{
		{name: "running job", job: "running", status: "active", promptReady: true, pending: 2, want: RuntimeBucketRunning},
		{name: "awaiting review", job: "awaiting_review", status: "active", promptReady: true, pending: 1, want: RuntimeBucketRunning},
		{name: "failed job still ready", job: "failed", status: "active", promptReady: true, pending: 1, want: RuntimeBucketReady},
		{name: "failed job idle shell", job: "failed", status: "active", pending: 1, want: RuntimeBucketIdle},
		{name: "settled completed", status: "completed", want: RuntimeBucketSettled},
		{name: "settled COMPLETED", status: "COMPLETED", promptReady: true, pending: 2, want: RuntimeBucketSettled},
		{name: "settled all phases done", status: "active", allDone: true, want: RuntimeBucketSettled},
		{name: "ready", status: "active", promptReady: true, pending: 2, want: RuntimeBucketReady},
		{name: "idle no prompt", status: "active", pending: 2, want: RuntimeBucketIdle},
		{name: "idle zero pending", status: "active", promptReady: true, want: RuntimeBucketIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyRuntimeBucket(tc.job, tc.status, tc.allDone, tc.promptReady, tc.pending)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBuildProgramSummaryRuntimeFields(t *testing.T) {
	h := &Handler{}

	running := h.buildProgramSummary("run-me", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title:  "Run Me",
			Status: "active",
			Phases: []PhaseBlueprint{
				{ID: "P1", Title: "One", Status: "pending", PromptTemplate: "do P1"},
			},
		},
		phases:    []Phase{{ID: "P1", Status: PhasePending}},
		activeJob: &Job{ID: "j1", PhaseID: "P1", Status: JobRunning},
	})
	if !running.Active {
		t.Fatal("catalog status active must set Active")
	}
	if running.RuntimeJobStatus != "running" || running.RuntimeBucket != RuntimeBucketRunning {
		t.Fatalf("running summary = %+v", running)
	}
	if running.PendingCount != 1 || !running.PromptReady {
		t.Fatalf("running pending/prompt = %+v", running)
	}

	awaiting := h.buildProgramSummary("await", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "Await", Status: "active",
			Phases: []PhaseBlueprint{{ID: "P1", PromptTemplate: "go"}},
		},
		phases:    []Phase{{ID: "P1", Status: PhasePending}},
		activeJob: &Job{ID: "j2", Status: JobAwaitingReview},
	})
	if awaiting.RuntimeBucket != RuntimeBucketRunning || awaiting.RuntimeJobStatus != "awaiting_review" {
		t.Fatalf("awaiting = %+v", awaiting)
	}

	failedReady := h.buildProgramSummary("fail-ready", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "Fail Ready", Status: "active",
			Phases: []PhaseBlueprint{{ID: "P1", PromptTemplate: "retry"}},
		},
		phases:    []Phase{{ID: "P1", Status: PhaseFailed}},
		activeJob: &Job{ID: "j3", Status: JobFailed},
	})
	if failedReady.RuntimeBucket != RuntimeBucketReady || failedReady.RuntimeJobStatus != "failed" {
		t.Fatalf("failed ready = %+v", failedReady)
	}

	settled := h.buildProgramSummary("done", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "Done", Status: "completed",
			Phases: []PhaseBlueprint{{ID: "P1", Status: "done"}},
		},
		phases: []Phase{{ID: "P1", Status: PhaseDone}},
	})
	if settled.RuntimeBucket != RuntimeBucketSettled || settled.PendingCount != 0 || settled.PromptReady || settled.Active {
		t.Fatalf("settled = %+v", settled)
	}

	settledProgress := h.buildProgramSummary("all-done", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "All Done", Status: "active",
			Phases: []PhaseBlueprint{{ID: "P1"}, {ID: "P2"}},
		},
		phases: []Phase{{ID: "P1", Status: PhaseDone}},
		state: &ProgramStateRecord{
			PhaseProgress: []PhaseProgressRecord{{PhaseID: "P2", Status: "done"}},
		},
	})
	if !settledProgress.AllPhasesDone || settledProgress.RuntimeBucket != RuntimeBucketSettled {
		t.Fatalf("all-done via progress = %+v", settledProgress)
	}

	ready := h.buildProgramSummary("ready", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "Ready", Status: "active",
			Phases: []PhaseBlueprint{
				{ID: "P1", Status: "done"},
				{ID: "P2", PromptTemplate: "  execute P2  "},
			},
		},
		phases: []Phase{{ID: "P1", Status: PhaseDone}, {ID: "P2", Status: PhasePending}},
	})
	if ready.RuntimeBucket != RuntimeBucketReady || !ready.PromptReady || ready.PendingCount != 1 {
		t.Fatalf("ready = %+v", ready)
	}

	idle := h.buildProgramSummary("idle", &programRuntime{
		blueprint: &ProgramBlueprint{
			Title: "Idle Shell", Status: "active",
			Phases: []PhaseBlueprint{{ID: "P1", PromptTemplate: "   "}},
		},
		phases: []Phase{{ID: "P1", Status: PhasePending}},
	})
	if idle.RuntimeBucket != RuntimeBucketIdle || idle.PromptReady || idle.PendingCount != 1 {
		t.Fatalf("idle = %+v", idle)
	}
}

func TestHandleProgramsRuntimeBucketAndArchived(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"live": {
				blueprint: &ProgramBlueprint{
					ID: "live", Title: "Live", Status: "active",
					Phases: []PhaseBlueprint{{ID: "P1", PromptTemplate: "do it"}},
				},
				phases:    []Phase{{ID: "P1", Status: PhasePending}},
				activeJob: &Job{ID: "j-live", Status: JobRunning},
			},
			"background": {
				blueprint: &ProgramBlueprint{
					ID: "background", Title: "Background", Status: "active",
					Phases: []PhaseBlueprint{{ID: "P1", PromptTemplate: "bg"}},
				},
				phases:    []Phase{{ID: "P1", Status: PhasePending}},
				activeJob: &Job{ID: "j-bg", Status: JobAwaitingReview},
			},
			"done": {
				blueprint: &ProgramBlueprint{
					ID: "done", Title: "Done", Status: "completed",
					Phases: []PhaseBlueprint{{ID: "P1"}},
				},
				phases: []Phase{{ID: "P1", Status: PhaseDone}},
			},
			"old": {
				blueprint: &ProgramBlueprint{
					ID: "old", Title: "Old", Status: "archived",
					Phases: []PhaseBlueprint{{ID: "P1"}},
				},
			},
		},
	}

	rec := httptest.NewRecorder()
	h.HandlePrograms(rec, httptest.NewRequest(http.MethodGet, "/api/v1/programs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body programsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programs) != 3 {
		t.Fatalf("default list=%+v", body.Programs)
	}
	byID := map[string]ProgramSummary{}
	for _, p := range body.Programs {
		if p.ID == "old" {
			t.Fatal("archived program leaked into default list")
		}
		byID[p.ID] = p
	}
	if byID["live"].RuntimeBucket != RuntimeBucketRunning || byID["live"].RuntimeJobStatus != "running" || !byID["live"].Active {
		t.Fatalf("live = %+v", byID["live"])
	}
	if byID["background"].RuntimeBucket != RuntimeBucketRunning || !byID["background"].Active {
		t.Fatalf("background persisted job must still bucket running: %+v", byID["background"])
	}
	if byID["done"].RuntimeBucket != RuntimeBucketSettled || byID["done"].Active {
		t.Fatalf("done = %+v", byID["done"])
	}
}
