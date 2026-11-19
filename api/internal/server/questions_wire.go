package server

import (
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// The Owner's questions: what the agent runtime asks and reads of them is
// questionbridge; the server constructs the runtime, mounts the routes and
// closes it.

func (s *Server) wireQuestions(dataDir string, workers bool) {
	rt := questionbridge.New(filepath.Join(dataDir, "questions"), s.audit)
	s.questionRuntime = rt
	if !workers || rt.Svc == nil {
		return
	}
	rt.StartSweep(approvalRetryInterval)
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
	if s.questionRuntime != nil {
		s.questionRuntime.Stop()
	}
}

func (s *Server) mountQuestions(r chi.Router) {
	if s.questionRuntime == nil {
		return
	}
	questions.Mount(r, s.auth, s.questionRuntime.Svc)
}

func (s *Server) wireQuestionAssessments() {
	if s.questionRuntime != nil && s.questionRuntime.Svc != nil {
		s.questionRuntime.Svc.Assessment = s.assessmentReader(assessmentreadings.AssessCard)
	}
}

// announceQuestionsOnBus hands the questions' transitions to the bus.
func (s *Server) announceQuestionsOnBus() {
	if s.questionRuntime != nil && s.questionRuntime.Svc != nil {
		s.questionRuntime.Svc.Announce = questionbridge.Announcer(func() eventlog.DB { return s.bus.db() })
	}
}
