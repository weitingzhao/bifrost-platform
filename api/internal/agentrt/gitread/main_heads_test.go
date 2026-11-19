package gitread

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; without the credential there is no read, and the cause is kept.
func TestMainHeadsSaysWhenThereIsNoAccess(t *testing.T) {
	boom := errors.New("no secret")
	_, err := MainHeads(func(context.Context) (Access, error) { return Access{}, boom }, &http.Client{})(context.Background(), "bifrost-platform")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
