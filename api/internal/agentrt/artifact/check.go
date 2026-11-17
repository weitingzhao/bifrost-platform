// Package artifact is the artifact store of the agent runtime: bodies kept
// by their content, the record of an upload, the search of their metadata
// and the two scans of a body (local paths, another task's workspace). It
// imports the bases (eventlog, actor) and no domain. The one thing of a
// domain it needs reaches it by registration: the check of the artifacts
// that are documents of a known protocol (a plan view, an external work
// description), which the package that knows those protocols registers at
// start (RegisterCheck).
package artifact

import (
	"errors"
	"sync/atomic"
)

// bodyCheck is the registered check: it refuses a body that says it is a
// document of a known protocol and is not a valid one.
var bodyCheck atomic.Pointer[func(kind string, body []byte) error]

// RegisterCheck installs the check every body passes before it is stored
// (PutArtifact, UploadArtifact). It is called once, from the init of the
// package that knows the view artifacts (the store's, planview.go).
func RegisterCheck(check func(kind string, body []byte) error) { bodyCheck.Store(&check) }

// ErrNoCheck: no check is registered, so a view artifact could not be told
// from a broken one, and nothing is stored.
var ErrNoCheck = errors.New("artifact: no check of view artifacts is registered; the artifact is not stored")

// checkBody runs the registered check. With none registered the body is
// refused: storing it unchecked could keep a plan view or an external work
// description that is not valid.
func checkBody(kind string, body []byte) error {
	f := bodyCheck.Load()
	if f == nil {
		return ErrNoCheck
	}
	return (*f)(kind, body)
}
