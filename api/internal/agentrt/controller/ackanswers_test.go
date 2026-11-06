package controller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

const (
	ackTask   = "t_01M6S77BXRBGYCB5GD5NRDH15N"
	ackOther  = "t_01M6S77BXRBGYCB5GD5NRDH15P"
	ackThread = "thread-elsewhere"
)

var ackAsked = time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)

// ackTaskState is a task as AckUsedAnswers reads it.
type ackTaskState struct {
	state   string
	stopped bool
}

// ackStore is a question store, the runtime's reach into it as the server
// wires it, and the tasks the controller reads.
type ackStore struct {
	t     *testing.T
	svc   *questions.Service
	now   time.Time
	tasks map[string]ackTaskState
	logs  []string
}

func newAckStore(t *testing.T) *ackStore {
	t.Helper()
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("OPS_VIEWER_ENV", "prod") // where the question store takes questions
	s := &ackStore{t: t, svc: questions.New(filepath.Join(t.TempDir(), "questions"), nil), now: ackAsked, tasks: map[string]ackTaskState{}}
	s.svc.SetClock(func() time.Time { return s.now })
	t.Cleanup(controller.SetTaskWaitForTest(func(_ context.Context, _ store.DB, task string) (string, bool, bool, error) {
		st, ok := s.tasks[task]
		return st.state, st.stopped, ok, nil
	}))
	return s
}

// ask asks a question as session about task (none when empty) and returns
// its id.
func (s *ackStore) ask(session, task, key string) string {
	s.t.Helper()
	req := map[string]any{"title": "Which way?", "urgency": "when you can", "meanwhile": "the work waits",
		"options": []map[string]string{{"label": "One"}, {"label": "Two"}}, "recommended": 1, "idempotency_key": key}
	if task != "" {
		req["task_id"], req["owner_duty"] = task, "goal"
	}
	raw, err := json.Marshal(req)
	must(s.t, err)
	res := s.svc.Create(ctx, session, session, "operator", raw)
	if res.Code != http.StatusCreated {
		s.t.Fatal("create:", res.Code, res.Body)
	}
	return res.Body.(map[string]any)["id"].(string)
}

func (s *ackStore) answer(id string) {
	s.t.Helper()
	if res := s.svc.Answer(ctx, "console-session", "owner", "admin", id, []byte(`{"option":"a","channel":"console"}`)); res.Code != http.StatusOK {
		s.t.Fatal("answer:", res.Code, res.Body)
	}
}

// read says whether the question's answer is acknowledged, and its status.
func (s *ackStore) read(id string) (bool, string) {
	s.t.Helper()
	q, ok, err := s.svc.Lookup(id)
	if err != nil || !ok {
		s.t.Fatal("lookup:", id, ok, err)
	}
	return q.Answer != nil && q.Answer.Read != nil, q.Status
}

// pass runs one AckUsedAnswers pass at at, with the runtime's Ack as the
// server wires it (questions_wire.go agentRuntimeAck).
func (s *ackStore) pass(at time.Time) int {
	s.t.Helper()
	s.now = at
	c := &controller.Controller{Now: func() time.Time { return at }, Infof: func(format string, args ...any) {
		s.logs = append(s.logs, fmt.Sprintf(format, args...))
	}}
	c.Sources.Unacked = questioncards.Unacked(s.svc)
	c.Sources.Ack = func(ctx context.Context, id string) error {
		res := s.svc.Ack(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", id, nil)
		if res.Code != http.StatusOK {
			return fmt.Errorf("the question store answered %d: %v", res.Code, res.Body)
		}
		return nil
	}
	n, err := c.AckUsedAnswers(ctx)
	must(s.t, err)
	return n
}

func TestAckUsedAnswersOfFinishedTasks(t *testing.T) {
	for _, tc := range []struct {
		name string
		task ackTaskState
		why  string
	}{
		{"accepted", ackTaskState{state: "accepted"}, "is accepted"},
		{"blocked", ackTaskState{state: "blocked"}, "is blocked"},
		{"cancelled", ackTaskState{state: "cancelled"}, "is cancelled"},
		{"stopped", ackTaskState{state: "running", stopped: true}, "a stop holds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAckStore(t)
			s.tasks[ackTask] = tc.task
			id := s.ask(questions.RuntimeSession, ackTask, "ack-finished-task-0001")
			s.answer(id)
			if n := s.pass(ackAsked.Add(time.Hour)); n != 1 {
				t.Fatal("acknowledged", n)
			}
			if read, status := s.read(id); !read || status != questions.StatusAnswered {
				t.Fatal("not acknowledged", read, status)
			}
			if len(s.logs) != 1 || !strings.Contains(s.logs[0], "Q1") || !strings.Contains(s.logs[0], tc.why) {
				t.Fatal("logged", s.logs)
			}
			// Once: an acknowledged answer is not listed again.
			if n := s.pass(ackAsked.Add(2 * time.Hour)); n != 0 || len(s.logs) != 1 {
				t.Fatal("acknowledged again", n, s.logs)
			}
		})
	}
}

func TestAckUsedAnswersLeavesARunningTasksRecentAnswer(t *testing.T) {
	s := newAckStore(t)
	s.tasks[ackTask] = ackTaskState{state: "running"}
	id := s.ask(questions.RuntimeSession, ackTask, "ack-running-task-0001")
	s.answer(id)
	if n := s.pass(ackAsked.Add(time.Hour)); n != 0 {
		t.Fatal("acknowledged", n)
	}
	if read, _ := s.read(id); read {
		t.Fatal("a task that waits on the answer had it acknowledged")
	}
	if len(s.logs) != 0 {
		t.Fatal("logged", s.logs)
	}
}

func TestAckUsedAnswersAfterADay(t *testing.T) {
	s := newAckStore(t)
	s.tasks[ackTask] = ackTaskState{state: "running"}
	id := s.ask(questions.RuntimeSession, ackTask, "ack-old-answer-00001")
	s.answer(id)
	if n := s.pass(ackAsked.Add(25 * time.Hour)); n != 1 {
		t.Fatal("acknowledged", n)
	}
	if read, _ := s.read(id); !read {
		t.Fatal("an answer 25 hours old was not acknowledged")
	}
	if len(s.logs) != 1 || !strings.Contains(s.logs[0], "answered 25h0m0s ago") {
		t.Fatal("logged", s.logs)
	}
}

func TestAckUsedAnswersWithoutATask(t *testing.T) {
	s := newAckStore(t)
	none := s.ask(questions.RuntimeSession, "", "ack-no-task-0000001")
	gone := s.ask(questions.RuntimeSession, ackOther, "ack-gone-task-00001")
	s.answer(none)
	s.answer(gone)
	if n := s.pass(ackAsked.Add(time.Minute)); n != 2 {
		t.Fatal("acknowledged", n)
	}
	for _, id := range []string{none, gone} {
		if read, _ := s.read(id); !read {
			t.Fatal("not acknowledged", id)
		}
	}
	logs := strings.Join(s.logs, "\n")
	if !strings.Contains(logs, "names no task") || !strings.Contains(logs, "does not exist") {
		t.Fatal("logged", s.logs)
	}
}

func TestAckUsedAnswersNeverAnOpenQuestion(t *testing.T) {
	s := newAckStore(t)
	s.tasks[ackTask] = ackTaskState{state: "accepted"}
	id := s.ask(questions.RuntimeSession, ackTask, "ack-open-question-01")
	if n := s.pass(ackAsked.Add(time.Hour)); n != 0 {
		t.Fatal("acknowledged", n)
	}
	if read, status := s.read(id); read || status != questions.StatusOpen {
		t.Fatal("an open question changed", read, status)
	}
}

func TestAckUsedAnswersNeverAnotherRequesters(t *testing.T) {
	s := newAckStore(t)
	s.tasks[ackTask] = ackTaskState{state: "accepted"}
	theirs := s.ask(ackThread, ackTask, "ack-other-requester1")
	old := s.ask(ackThread, "", "ack-other-requester2")
	s.answer(theirs)
	s.answer(old)
	if n := s.pass(ackAsked.Add(48 * time.Hour)); n != 0 {
		t.Fatal("acknowledged", n)
	}
	for _, id := range []string{theirs, old} {
		if read, _ := s.read(id); read {
			t.Fatal("another requester's answer was acknowledged", id)
		}
	}
}

func TestAckUsedAnswersLeavesARecentRuleAnswerToItsPass(t *testing.T) {
	s := newAckStore(t)
	s.tasks[ackTask] = ackTaskState{state: "blocked"}
	id := s.ask(questions.RuntimeSession, ackTask, "ack-rule-answer-0001")
	if res := s.svc.AnswerAsRule(ctx, questions.RuntimeSession, "card-auto-answer", id, "a", ackAsked.Add(time.Hour), ""); res.Code != http.StatusOK {
		t.Fatal("rule answer:", res.Code, res.Body)
	}
	if n := s.pass(ackAsked.Add(time.Minute)); n != 0 {
		t.Fatal("acknowledged", n)
	}
	if n := s.pass(ackAsked.Add(25 * time.Hour)); n != 1 {
		t.Fatal("acknowledged", n)
	}
}
