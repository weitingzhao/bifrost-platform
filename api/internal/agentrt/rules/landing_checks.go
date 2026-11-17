package rules

// LandingChecksVersion identifies the coordinator v3 checks, including §12.
// It is audit evidence, not a substitute for recomputing the refusal set.
const LandingChecksVersion = 3

type LandingRefusal string

const (
	LandingFloorSoft    LandingRefusal = "floor_soft"
	LandingRulePaths    LandingRefusal = "rule_paths"
	LandingPresentation LandingRefusal = "presentation"
	LandingCore         LandingRefusal = "core"
	// LandingFloorGrant: a floor-grantable path (FloorEntry: infra's k8s/**,
	// scripts/**, ...; platform's floor paths neither hard nor open). Only
	// approve_floor_landing waives it, with rule_paths.
	LandingFloorGrant LandingRefusal = "floor_grant"
)
