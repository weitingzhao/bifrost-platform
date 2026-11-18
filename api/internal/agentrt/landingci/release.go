package landingci

const PlatformRepoURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"

// The api-scope result of a ci-platform run: what its test-api step ran.
// A run without it predates the result and ran everything (full).
const (
	APIScopeFull = "full"
)
