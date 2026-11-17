// Package runhooks lets a reader of pipeline runs be woken by the bus
// instead of listing them on a timer (2026-11-16, the gate readers' switch:
// the CI line writes the handling, this package gives it the durable
// consumer). A hook names itself, a pipeline-name prefix and a handler; the
// workers' bus runs one durable consumer of ci.run.finished per hook and
// calls the handler with the run's name and commit for each matching run. A
// hook registered before the bus starts is started with it; one registered
// after is started at once.
package runhooks

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Handler handles one finished run; an error asks for the event again.
type Handler func(ctx context.Context, run, sha string) error

// Hook is one reader to wake.
type Hook struct {
	Name, Prefix string
	Handle       Handler
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// Starter starts a hook's consumer (installed by the bus when it runs).
type Starter func(Hook) error

// Registry holds the hooks of one process.
type Registry struct {
	mu      sync.Mutex
	hooks   []Hook
	starter Starter
}

// Register adds a hook. Its durable consumer is named run-<name>.
func (r *Registry) Register(h Hook) error {
	if !nameRE.MatchString(h.Name) || h.Handle == nil || h.Prefix == "" {
		return fmt.Errorf("runhooks: a hook needs a name (lowercase, digits, dashes), a prefix and a handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.hooks {
		if x.Name == h.Name {
			return fmt.Errorf("runhooks: %s registered twice", h.Name)
		}
	}
	r.hooks = append(r.hooks, h)
	if r.starter != nil {
		return r.starter(h)
	}
	return nil
}

// Start installs the bus's starter and starts every hook registered so far.
func (r *Registry) Start(s Starter) []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starter = s
	var errs []error
	for _, h := range r.hooks {
		if err := s(h); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", h.Name, err))
		}
	}
	return errs
}

// Consumer is a hook's durable consumer name.
func (h Hook) Consumer() string { return "run-" + h.Name }

// Split reads ci.run.finished's subject "<run>@<sha>".
func Split(subject string) (run, sha string, ok bool) {
	i := strings.LastIndex(subject, "@")
	if i <= 0 || i == len(subject)-1 {
		return "", "", false
	}
	return subject[:i], subject[i+1:], true
}

// Deliver is a hook's handling of one envelope subject: nil acknowledges (also
// when the run is not this hook's), an error asks for it again.
func Deliver(ctx context.Context, h Hook, subject string) error {
	run, sha, ok := Split(subject)
	if !ok || !strings.HasPrefix(run, h.Prefix) {
		return nil
	}
	return h.Handle(ctx, run, sha)
}
