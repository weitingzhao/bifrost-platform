package agentrtapi_test

import "os"

// The intake token of the fixture configuration (intake_tokens:), set for the
// whole test binary. Its routes were removed with business issues (the Owner
// 2026-11-15); the role stays in the configuration's ladder, so the tests
// keep one such token to show it reaches nothing.
const (
	fixtureIntakeEnv   = "AGENTRTAPI_FIXTURE_INTAKE_TOKEN"
	fixtureIntakeToken = "fixture-intake-1"
)

func init() {
	if err := os.Setenv(fixtureIntakeEnv, fixtureIntakeToken); err != nil {
		panic(err)
	}
}
