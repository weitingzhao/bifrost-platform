package store

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func strictViewValue(v any, out any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return strictViewJSON(raw, out)
}
func strictViewJSON(raw []byte, out any) error {
	if _, err := canon.Parse(raw); err != nil {
		return invalid("invalid JSON: %v", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return invalid("invalid fields: %v", err)
	}
	return nil
}
