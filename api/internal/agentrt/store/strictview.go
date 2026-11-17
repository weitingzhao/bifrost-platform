package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
)

// The strict reading of a JSON document and the shape of a digest are
// package strictjson's (they were this file); these are its names as this
// package has always written them, for the vocabulary (vocab.go), the
// external work's writes (externalwork.go) and the wiring of the plan
// code's ports (portsbind.go).
var shaPattern = strictjson.SHAPattern

func strictViewValue(v any, out any) error { return strictjson.Value(v, out) }
