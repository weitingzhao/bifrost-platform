package server

import (
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
)

// mainOf is the one way display readers ask where main of repo is
// (bushook.MainOf): the ledger where this environment keeps it, else fallback.
func (s *Server) mainOf(reader string, fallback bushook.MainReader) bushook.MainReader {
	return bushook.MainOf(s.bus.gitLedger.Load(), s.bus.db, reader, fallback)
}
