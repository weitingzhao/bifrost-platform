package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
)

// The strict reading of a JSON document is package strictjson's (it was
// this file); this is its name as this package has always written it, for
// the vocabulary (vocab.go) and the wiring of the plan code's ports
// (portsbind.go).
func strictViewValue(v any, out any) error { return strictjson.Value(v, out) }
