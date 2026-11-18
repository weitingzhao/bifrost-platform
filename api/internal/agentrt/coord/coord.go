// Package coord is the coordination domain: what actors use so they do not
// collide. Messages between actors and their mailboxes, the controls that
// stop, pause or drain, and the seats' registration. It imports the bases
// (eventlog, actor) and no other domain; the store, which still holds the
// other domains, reaches it by import and gives it two things by
// registration: where a message's body is stored and the check of a text
// that claims an approval.
package coord

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// NewMessageID and newControlID mint ids in the shapes the tables check.
func NewMessageID() string { return "m_" + eventlog.NewULID(time.Now()) }
func newControlID() string { return "c_" + eventlog.NewULID(time.Now()) }

// artifactStore stores a message's body as an artifact and returns its id;
// claims records a text that claims an approval (AlertClaim, claims.go).
// Both reach this package by registration from the store's init (Register).
var (
	artifactStore atomic.Pointer[func(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error)]
	claims        atomic.Pointer[func(ctx context.Context, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error]
)

// Register installs the artifact store and the claim check. It is called
// once, from the init of the package that owns artifacts and policy alerts.
func Register(put func(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error),
	claim func(ctx context.Context, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error) {
	artifactStore.Store(&put)
	claims.Store(&claim)
}

// putArtifact stores a message's body. With no store registered a message
// cannot be kept, so it is refused.
func putArtifact(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error) {
	f := artifactStore.Load()
	if f == nil {
		return "", errors.New("coord: no artifact store is registered; the message is not sent")
	}
	return (*f)(ctx, tx, actor, kind, mediaType, body)
}

// claimCheck records a message that says the Owner approved. With no check
// registered the message is refused: sending it unrecorded would lose the
// alert.
func claimCheck(ctx context.Context, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error {
	f := claims.Load()
	if f == nil {
		return errors.New("coord: no check of approval claims is registered; the message is not sent")
	}
	return (*f)(ctx, tx, ev, cause, source, artifact, body)
}
