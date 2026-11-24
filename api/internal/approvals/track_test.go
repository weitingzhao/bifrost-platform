package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 2026-11-23, Owner (relayed by 1 Plan): a change of law is approved through
// its own channel on Ops, apart from the daily cards. The list splits by
// track so the Console can give law_amend its own entry.
func TestListSplitsLawFromDailyCards(t *testing.T) {
	s, _ := lifetimeService(t)
	daily := s.create(context.Background(), "track", "trigger_cnpg_backup", "backup", "", nil)
	law := s.create(context.Background(), "track", "law_amend", "law", "", map[string]any{
		"op": "repeal", "law": "1", "before": "old text", "revert": "add it again",
	})
	if daily.Status != http.StatusCreated || law.Status != http.StatusCreated {
		t.Fatalf("create daily=%d %v law=%d %v", daily.Status, daily.Body, law.Status, law.Body)
	}
	list := func(q string) (int, []map[string]any) {
		w := httptest.NewRecorder()
		s.HandleList(w, httptest.NewRequest(http.MethodGet, "/api/v1/approvals?"+q, nil))
		var out struct{ Approvals []map[string]any }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Approvals
	}
	if code, all := list(""); code != 200 || len(all) != 2 {
		t.Fatalf("no track = %d %d cards", code, len(all))
	}
	if _, l := list("track=law"); len(l) != 1 || l[0]["action"] != "law_amend" || l[0]["track"] != TrackLaw {
		t.Fatalf("track=law = %v", l)
	}
	if _, d := list("track=daily"); len(d) != 1 || d[0]["action"] != "trigger_cnpg_backup" || d[0]["track"] != TrackDaily {
		t.Fatalf("track=daily = %v", d)
	}
	if code, _ := list("track=other"); code != http.StatusBadRequest {
		t.Fatalf("unknown track = %d", code)
	}
}
