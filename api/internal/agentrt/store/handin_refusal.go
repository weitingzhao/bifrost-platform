package store

import ()

// handinRefusal carries only the rejected proposal's local reference.
type handinRefusal struct {
	task string
	err  error
}

func (e *handinRefusal) Error() string { return e.err.Error() }
func (e *handinRefusal) Unwrap() error { return e.err }
