package cidomains

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; the read of Gitea's commit listing is held here.
func TestChangedFilesFromReadsGiteasCommit(t *testing.T) {
	commit := strings.Repeat("c", 40)
	var asked string
	var limit int64
	got, err := ChangedFilesFrom(func(_ context.Context, path string, l int64) ([]byte, error) {
		asked, limit = path, l
		return []byte(`{"sha":"` + commit + `","files":[{"filename":"api/a.go"}]}`), nil
	})(context.Background(), "a/b", commit)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a%2Fb/git/commits/" + commit + "?stat=false&verification=false&files=true"; asked != want || limit != 8<<20 {
		t.Fatalf("asked %q limit %d, want %q", asked, limit, want)
	}
	if !reflect.DeepEqual(got, []string{"api/a.go"}) {
		t.Fatalf("got %v", got)
	}
	boom := errors.New("gitea down")
	if _, err := ChangedFilesFrom(func(context.Context, string, int64) ([]byte, error) { return nil, boom })(context.Background(), "r", commit); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
