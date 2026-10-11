package server

import (
	"context"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// questionRuntime is the only server-package holder of the questions service.
type questionRuntime struct {
	svc *questions.Service
}

func (s *Server) wireQuestions(dataDir string, workers bool) {
	svc := questions.New(filepath.Join(dataDir, "questions"), s.audit)
	s.questionRuntime = &questionRuntime{svc: svc}
	if !workers || svc == nil {
		return
	}
	safego.Go("questions.sweep", func() {
		svc.RunSweep(context.Background(), approvalRetryInterval)
	})
}

func (s *Server) mountQuestions(r chi.Router) {
	if s.questionRuntime == nil {
		return
	}
	questions.Mount(r, s.auth, s.questionRuntime.svc)
}
