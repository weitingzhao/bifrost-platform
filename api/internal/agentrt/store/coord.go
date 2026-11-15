package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// The coordination domain (messages, controls, seats) is package coord. It
// imports the bases and not this package. These are its names as this
// package has always exported them, and the two things of this package it
// needs, registered at start: where a message's body is stored (PutArtifact)
// and the record of a text that claims an approval (alertClaim).

func init() { coord.Register(PutArtifact, alertClaim) }

// MaxMessageBody is the largest message body (coord.MaxMessageBody).
const MaxMessageBody = coord.MaxMessageBody

// Message is a message as its receiver reads it.
type Message = coord.Message

func SendMessage(ctx context.Context, db DB, w Writer, to, kind string, body []byte) (string, error) {
	return coord.SendMessage(ctx, db, w, to, kind, body)
}

func Unread(ctx context.Context, db DB, w Writer, limit int) ([]Message, error) {
	return coord.Unread(ctx, db, w, limit)
}

func AckMessages(ctx context.Context, db DB, w Writer, ids []string) ([]string, error) {
	return coord.AckMessages(ctx, db, w, ids)
}

func Thread(ctx context.Context, db DB, actor, with string, limit int) ([]Message, error) {
	return coord.Thread(ctx, db, actor, with, limit)
}

func PostMessage(ctx context.Context, tx pgx.Tx, from, to, kind string, body []byte) (string, error) {
	return coord.PostMessage(ctx, tx, from, to, kind, body)
}

func AckMessage(ctx context.Context, tx pgx.Tx, actor, messageID string) error {
	return coord.AckMessage(ctx, tx, actor, messageID)
}

func RequestControl(ctx context.Context, db DB, w Writer, key, scope, ref, kind, reason string) (map[string]any, error) {
	return coord.RequestControl(ctx, db, w, key, scope, ref, kind, reason)
}

func LiftControl(ctx context.Context, db DB, actor, id string) (map[string]any, error) {
	return coord.LiftControl(ctx, db, actor, id)
}

func SetControl(ctx context.Context, tx pgx.Tx, actor, controlID, scope, scopeRef, kind, reason string) error {
	return coord.SetControl(ctx, tx, actor, controlID, scope, scopeRef, kind, reason)
}

func EnsureSeats(ctx context.Context, db DB, configured []string) ([]string, error) {
	return coord.EnsureSeats(ctx, db, configured)
}
