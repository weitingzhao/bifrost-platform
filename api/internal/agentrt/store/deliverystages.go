package store

import "time"

// Delivery stages: a commit's way from handed in to on PROD, with when each
// stage was entered and left. FoldExternalDelivery (delivery_external.go)
// fills them from recorded pipeline runs; what the platform does not record
// is unknown with a reason, never inferred.

// The seven stages, always in this order.
const (
	StageHandedIn    = "handed_in"
	StageLandingCI   = "landing_ci"
	StageOwnerWait   = "owner_wait"
	StageOnMain      = "on_main"
	StageSTG         = "stg"
	StageProdRelease = "prod_release"
	StageOnProd      = "on_prod"
)

// DeliveryStageOrder is the stages a delivery answer carries, in order.
var DeliveryStageOrder = []string{StageHandedIn, StageLandingCI, StageOwnerWait, StageOnMain, StageSTG, StageProdRelease, StageOnProd}

// A stage's state.
const (
	StageDone       = "done"
	StageCurrent    = "current"
	StageNotReached = "not_reached"
	StageUnknown    = "unknown"
)

// DeliveryStage is one stage of a change's delivery.
type DeliveryStage struct {
	Stage     string    `json:"stage"`
	State     string    `json:"state"`
	EnteredAt time.Time `json:"entered_at,omitzero"`
	LeftAt    time.Time `json:"left_at,omitzero"`
	Actor     string    `json:"actor,omitempty"`
	// ActorRole: executor, approver, pusher or releaser.
	ActorRole string `json:"actor_role,omitempty"`
	WaitsOn   string `json:"waits_on,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Source    string `json:"source,omitempty"`
}

// DeliveryProduction is a current release record validated against matching
// STG build digests and settled PROD pods. Reason means the read is unknown.
// It is a read result, not a fabricated deploy_seen event or release gate.
type DeliveryProduction struct {
	Commit                 string
	StartedAt, CompletedAt time.Time
	Reason                 string
	// NotCommit, set only beside a Reason: the commit of the newest STG
	// release, when the settled PROD workloads were read and run another
	// build. PROD does not run that commit yet.
	NotCommit string
}
