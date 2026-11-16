package store

import (
	"regexp"
)

// The digest check is still used by presence records.

var (
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)
