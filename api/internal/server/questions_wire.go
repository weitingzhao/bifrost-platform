package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
	ownerassembly "github.com/weitingzhao/bifrost-platform/api/internal/owner/assembly"
)

// The Owner's questions: what the agent runtime asks and reads of them is
// questionbridge; the owner's assembly (owner/assembly, since 2026-11-19)
// constructs the runtime and mounts the routes; the server keeps it, binds
// it to the assessments and the bus, and closes it.

func (s *Server) wireQuestions(dataDir string, workers bool) {
	s.questionRuntime = ownerassembly.Questions(dataDir, s.audit, workers, approvalRetryInterval)
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
	ownerassembly.MountQuestions(r, s.auth, s.questionRuntime)
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
