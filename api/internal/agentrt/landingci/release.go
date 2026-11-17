package landingci

import "time"

const PlatformRepoURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"

// ReleaseEvidence discovers U and infra from CI of exactly P. It does not
// assert they equal a previously frozen snapshot (the controller owns that).
type ReleaseEvidence struct {
	Evidence
	PipelineRef, RepoURL string
	TestedBy             string
	ReadAt               time.Time
	// Undecided names the runs of P newer than the selected one that have no
	// verdict yet (R5:5.7-2). They were passed over, never trusted.
	Undecided []string
	// Partial names the greens of P that tested only part of the API
	// (api-scope=console). They neither vouch for P nor hide another run.
	Partial []string
	// Stale is retained for historical observations. New reads never set it.
	Stale []string
	// LastFull* is the newest full-scope green of any revision (any P); a
	// zero LastFullAt means none was found.
	LastFullRun, LastFullCommit string
	LastFullAt                  time.Time
}

// FullMaxAge remains the landing reuse freshness cap and the historical
// observation cap. It no longer decides STG eligibility.
const FullMaxAge = 48 * time.Hour

// The api-scope result of a ci-platform run: what its test-api step ran.
// A run without it predates the result and ran everything (full).
const (
	APIScopeResult = "api-scope"
	// What the run's test-api tested: its packages (relative to the api
	// module, space separated), the store package's -run pattern ("" for
	// all of it) and why it chose its scope.
	APIPackagesResult = "api-packages"
	APIStoreRunResult = "api-store-run"
	APIScopeWhyResult = "api-scope-why"
	APIScopeFull      = "full"
	APIScopeConsole   = "console"
	// An incremental run tested the API packages its change reaches (R5:5.7-1).
	APIScopeIncremental = "incremental"
)
