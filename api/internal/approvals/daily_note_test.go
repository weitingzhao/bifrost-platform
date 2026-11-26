package approvals

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// 2026-11-25, Owner issues ISSUE-4/ISSUE-5 (relayed by 0 Legislation): cards
// that are neither law tickets nor six-class kept reaching his phone (27 daily
// cards #722–#767 after LAW-6 二 took effect, none carried owner_class). The
// requesting thread is told at create time, in the answer it reads back.
func TestADailyCardTellsItsRequesterItShouldNotReachTheOwner(t *testing.T) {
	svc, _ := lifetimeService(t)
	create := func(body string) map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/approvals", strings.NewReader(body))
		w := httptest.NewRecorder()
		svc.HandleCreate(w, req.WithContext(actuation.WithPrincipal(req.Context(), actuation.Principal{Role: actuation.RoleOperator})))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", body, w.Code, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	daily := create(`{"action":"ib_maintenance","reason":"backup"}`)
	if daily["track"] != TrackDaily || daily["to_owner"] != DailyCardNote {
		t.Fatalf("daily card answer = %v", daily)
	}
	// Positive control for the case above: the same action claimed as one
	// of the six carries no note.
	six := create(`{"action":"ib_maintenance","reason":"backup","owner_class":"H3"}`)
	if six["owner_class"] != "H3" || six["to_owner"] != "" {
		t.Fatalf("six-class answer = %v", six)
	}
	law := create(`{"action":"law_amend","reason":"law","params":{"op":"repeal","law":"1","before":"old text","revert":"add it again"}}`)
	if law["track"] != TrackLaw || law["to_owner"] != "" {
		t.Fatalf("law ticket answer = %v", law)
	}
}
