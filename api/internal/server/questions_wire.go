package server

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// questionRuntime is the only server-package holder of the questions service.
// The approvals retry loop is not a model for stopping this one: it is started
// with context.Background and a ticker that waits out the first interval, so
// a short test never enters it. This sweep used to fire immediately.
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

// Close stops the questions sweep and waits until that goroutine has returned.
// Other background loops are unchanged.
func (s *Server) Close() {
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
