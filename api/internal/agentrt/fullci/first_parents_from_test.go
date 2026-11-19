package fullci

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; the read of Gitea's commit page is held here.
func TestFirstParentsFromReadsGiteasPageOfCommits(t *testing.T) {
	green, red := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var asked string
	var limit int64
	got, err := FirstParentsFrom(func(_ context.Context, path string, l int64) ([]byte, error) {
		asked, limit = path, l
		return []byte(`[{"sha":"` + red + `","parents":[{"sha":"` + green + `"}],"commit":{"message":"subject\nbody"}}]`), nil
	})(context.Background(), "a/b", green, red, 5)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a%2Fb/commits?sha=" + red + "&limit=100&stat=false&verification=false&files=false"; asked != want || limit != 8<<20 {
		t.Fatalf("asked %q limit %d, want %q", asked, limit, want)
	}
	if want := [][2]string{{red, "subject"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	boom := errors.New("gitea down")
	if _, err := FirstParentsFrom(func(context.Context, string, int64) ([]byte, error) { return nil, boom })(context.Background(), "r", green, red, 5); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
