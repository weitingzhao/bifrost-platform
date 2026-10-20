//go:build !linux && !darwin

package agentd

import "errors"

// procIdentity: this system gives no process identity finer than ps's start
// time, so an exam run's group is never ended by its record alone here.
func procIdentity(int) (string, error) {
	return "", errors.New("agentd: no process identity on this system")
}
