// Package strictjson reads a JSON document into a struct and refuses what
// the struct does not name: a value that is not canonical JSON (a float, a
// duplicate key, trailing data) and a field the struct has no place for are
// both an invalid request (eventlog.ErrInvalid). It also holds the shape of a
// commit or content digest those documents carry. It knows no document of
// its own: the plan's view records, the plan's observations, the external
// work note and the metric definitions each pass their own struct.
//
// It was store's strictview.go and is the same code: the plan code, the
// store's vocabulary and the store's wiring of external work all read
// through it, and the plan code may not import store.
package strictjson

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// SHAPattern is a hexadecimal digest of 40 or of 64 digits: a Git commit, or
// a SHA-256.
var SHAPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Value reads v, a decoded JSON value, into out as JSON does.
func Value(v any, out any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return JSON(raw, out)
}

// JSON reads raw into out.
func JSON(raw []byte, out any) error {
	if _, err := canon.Parse(raw); err != nil {
		return eventlog.Invalid("invalid JSON: %v", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return eventlog.Invalid("invalid fields: %v", err)
	}
	return nil
}
