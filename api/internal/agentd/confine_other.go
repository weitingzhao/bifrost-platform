//go:build !linux

package agentd

import "errors"

// errNoWall: the write wall is Landlock's, which only Linux has; elsewhere
// no executor is walled, and none shares the host.
var errNoWall = errors.New("agentd: no write wall on this system (Landlock is Linux's)")

func landlockABI() (int, error) { return 0, errNoWall }

func restrictWrites([]string) error { return errNoWall }
