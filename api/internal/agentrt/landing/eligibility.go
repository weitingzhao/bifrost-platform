package landing

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/mainhistory"

// The answer to "is this result on main, and in effect" is package
// mainhistory's since 2026-11-17 (the plan's completion atoms read it and may
// not import this package). These are its names as this package has always
// exported them: the same types and values, not copies.

type History = mainhistory.History

const (
	Unknown   = mainhistory.Unknown
	Never     = mainhistory.Never
	Effective = mainhistory.Effective
	Reverted  = mainhistory.Reverted
)

type MainHistory = mainhistory.MainHistory
