package store

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"

// Compatibility names for the delivery read model; readers retain their API.
const (
	StageHandedIn    = deliveryview.StageHandedIn
	StageLandingCI   = deliveryview.StageLandingCI
	StageOwnerWait   = deliveryview.StageOwnerWait
	StageOnMain      = deliveryview.StageOnMain
	StageSTG         = deliveryview.StageSTG
	StageProdRelease = deliveryview.StageProdRelease
	StageOnProd      = deliveryview.StageOnProd
	StageDone        = deliveryview.StageDone
	StageCurrent     = deliveryview.StageCurrent
	StageNotReached  = deliveryview.StageNotReached
	StageUnknown     = deliveryview.StageUnknown
)

var DeliveryStageOrder = deliveryview.DeliveryStageOrder

type DeliveryStage = deliveryview.DeliveryStage
type DeliveryProduction = deliveryview.DeliveryProduction
