// Package approvalhome pins human decisions to the PROD control plane.
package approvalhome

import (
	"os"
	"strings"
)

const Required = "Human requests must be created and approved on the PROD platform at ops.bifrost.lan."

// IsProd uses the deployed platform identity, never a request's target env,
// Host header or Console URL. Both API and workers deployments set this value.
// Unset/unknown identities fail closed, including local development.
func IsProd() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("OPS_VIEWER_ENV"))) == "prod"
}
