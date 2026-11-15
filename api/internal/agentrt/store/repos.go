package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/infracheck"
)

// Repo retains repository metadata for plan validation and legacy records.
// Verify, HandIn, Setup, Siblings and TestDB describe the retired dispatcher
// setup; this table does not run an executor or define current CI policy.
type Repo struct {
	Verify   [][]string
	HandIn   [][]string
	TestDirs []string
	Setup    [][]string
	Siblings []Sibling
	Core     []string
	TestDB   bool
}

// Sibling is a repository beside a task's checkout, at the head of Ref.
type Sibling struct {
	Repo, Ref string
}

// Repos are the repositories goals and plans may name. TestDirs bounds
// plan checks; legacy setup and verification fields keep their stored shape.
var Repos = map[string]Repo{
	"bifrost-platform": {
		Verify: [][]string{
			{"go", "-C", "api", "build", "./..."},
			{"go", "-C", "api", "vet", "./..."},
			{"go", "-C", "api", "run", "./cmd/agentrt-check", "rules"},
			{"go", "-C", "api", "run", "./cmd/agentrt-check", "changed"},
			{"npm", "--prefix", "console", "run", "lint"},
			{"npm", "--prefix", "console", "test"},
			{"npm", "--prefix", "console", "run", "build"},
		},
		HandIn: [][]string{
			{"go", "-C", "api", "build", "./..."},
			{"go", "-C", "api", "vet", "./..."},
			{"npm", "--prefix", "console", "run", "lint"},
		},
		TestDirs: []string{"api"},
		Setup: [][]string{
			{"go", "-C", "api", "mod", "download"},
			{"npm", "--prefix", "../bifrost-ui", "ci", "--no-audit", "--no-fund"},
			{"npm", "--prefix", "console", "ci", "--no-audit", "--no-fund"},
		},
		Siblings: []Sibling{{Repo: "bifrost-ui", Ref: "main"}},
		Core:     []string{"api/internal/agentrt", "api/internal/agentd", "api/cmd/agentd"},
		TestDB:   true,
	},
	"bifrost-trade-infra": {
		Verify: [][]string{{"sh", "-c", infracheck.Check()}},
		HandIn: [][]string{{"sh", "-c", infracheck.Check()}},
		Setup:  infracheck.Setup(""),
	},
}

// SpecTimeoutS is how long an implementation task's check may run, all of
// its commands.
const SpecTimeoutS = 1500
