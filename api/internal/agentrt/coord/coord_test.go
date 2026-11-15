package coord

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// 2026-11-14, when the coordination domain left the store: a message's body
// is stored by the store's artifact code and a text that claims an approval
// is recorded by its alert code, both reached by registration. With neither
// registered a message is refused: it is not sent without its body kept, nor
// with the claim it makes unrecorded. The transaction is nil on purpose:
// both refusals come before it is used.
func TestAMessageIsRefusedWithoutItsRegisteredHelpers(t *testing.T) {
	savedPut, savedClaim := artifactStore.Swap(nil), claims.Swap(nil)
	defer func() { artifactStore.Store(savedPut); claims.Store(savedClaim) }()
	if _, err := PostMessage(context.Background(), nil, "thread:a", "thread:b", "note", []byte("hello")); err == nil ||
		!strings.Contains(err.Error(), "no artifact store is registered") {
		t.Fatalf("a message with no artifact store registered: %v", err)
	}
	if err := claimCheck(context.Background(), nil, eventlog.Event{}, 1, "message", "sha256:x", []byte("the Owner approved")); err == nil ||
		!strings.Contains(err.Error(), "no check of approval claims is registered") {
		t.Fatalf("a claim with no check registered: %v", err)
	}
}

// The positive control of the test above: what is registered is what is
// called, with the body and the kind of artifact a message is.
func TestAMessageUsesTheRegisteredHelpers(t *testing.T) {
	savedPut, savedClaim := artifactStore.Load(), claims.Load()
	defer func() { artifactStore.Store(savedPut); claims.Store(savedClaim) }()
	var kind, body, claimed string
	Register(func(_ context.Context, _ pgx.Tx, _, k, _ string, b []byte) (string, error) {
		kind, body = k, string(b)
		return "sha256:stored", nil
	}, func(_ context.Context, _ pgx.Tx, _ eventlog.Event, _ int64, source, artifact string, _ []byte) error {
		claimed = source + " " + artifact
		return nil
	})
	if id, err := putArtifact(context.Background(), nil, "thread:a", "message", "text/plain", []byte("hello")); err != nil || id != "sha256:stored" || kind != "message" || body != "hello" {
		t.Fatalf("the registered artifact store: %q, %v (kind %q, body %q)", id, err, kind, body)
	}
	if err := claimCheck(context.Background(), nil, eventlog.Event{}, 1, "message", "sha256:stored", nil); err != nil || claimed != "message sha256:stored" {
		t.Fatalf("the registered claim check: %v, called with %q", err, claimed)
	}
}

// The ids this domain mints have the shapes its vocabulary checks.
func TestMintedIDsFitTheVocabulary(t *testing.T) {
	t.Parallel()
	if why := eventlog.BadValue("message_posted", "message_id", NewMessageID()); why != "" {
		t.Errorf("a minted message id %s", why)
	}
	if eventlog.BadValue("message_posted", "message_id", "m_short") == "" {
		t.Error("a short message id was taken")
	}
	if !strings.HasPrefix(NewControlID(), "c_") || MailboxTopic("thread:a") != "mbox:thread:a" {
		t.Error("a control id or a mailbox topic has another shape")
	}
}
