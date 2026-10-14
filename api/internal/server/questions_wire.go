package server

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// questionRuntime is the only server-package holder of the questions service.
// This sweep used to fire immediately; the runtime owns its cancellation.
type questionRuntime struct {
	svc    *questions.Service
	cancel context.CancelFunc
	done   chan struct{}
	stop   sync.Once
}

func (s *Server) wireQuestions(dataDir string, workers bool) {
	svc := questions.New(filepath.Join(dataDir, "questions"), s.audit)
	rt := &questionRuntime{svc: svc}
	s.questionRuntime = rt
	if !workers || svc == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.cancel = cancel
	rt.done = make(chan struct{})
	safego.Go("questions.sweep", func() {
		defer close(rt.done)
		svc.RunSweep(ctx, approvalRetryInterval)
	})
}

// Close cancels approval result flushing without waiting on storage I/O,
// then stops the questions sweep. Other background loops are unchanged.
func (s *Server) Close() {
	s.closeAgentRuntime()
	if s.approvalsCancel != nil {
		s.approvalsCancel()
	}
	if s.approvals != nil {
		// A result held by this process gets one last write before it exits.
		s.approvals.FlushHeldFinishes()
	}
	rt := s.questionRuntime
	if rt == nil {
		return
	}
	rt.stop.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.done != nil {
			<-rt.done
		}
	})
}

func (s *Server) mountQuestions(r chi.Router) {
	if s.questionRuntime == nil {
		return
	}
	questions.Mount(r, s.auth, s.questionRuntime.svc)
}

// agentRuntimeQuestion reads a question for the agent runtime's controller
// (agentRuntimeSources) by Lookup, which writes nothing; nil without a
// question store.
func (rt *questionRuntime) agentRuntimeQuestion() func(context.Context, string) (store.Question, bool) {
	if rt == nil || rt.svc == nil {
		return nil
	}
	return func(ctx context.Context, id string) (store.Question, bool) {
		q, ok, err := within(ctx, func() (questions.Question, bool, error) { return rt.svc.Lookup(id) })
		if err != nil || !ok {
			return store.Question{}, false
		}
		var option, text, by string
		var at time.Time
		if q.Answer != nil {
			option, text, by, at = q.Answer.Option, q.Answer.Text, q.Answer.By, q.Answer.At
		}
		return questionFact(q.ID, q.Status, q.Status == questions.StatusAnswered, option, text, by, at)
	}
}
