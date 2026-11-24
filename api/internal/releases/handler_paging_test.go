package releases

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// 2026-11-23, the Console's ask: PROD keeps 1,673 release records, read whole
// at 1.1 MB, and the History table could not page or say how many there are.
// Paging back with next visits every record of the filter once, in order,
// including records completed at the same moment; total counts them all.
// Control: a ?before= that names no record is an error, not the first page.
func TestReleasesPageBackWithNextAndCountTheTotal(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	var rows []Record
	for i := 0; i < 7; i++ {
		at := t0.Add(time.Duration(i/2) * time.Minute) // pairs complete at the same time
		rows = append(rows, Record{Run: fmt.Sprintf("run-%d", i), Lane: "platform", Env: "stg", CompletedAt: at})
	}
	rows = append(rows, Record{Run: "other", Lane: "app", Env: "stg", CompletedAt: t0.Add(time.Hour)})
	s, _ := newFakeService(t)
	s.SetTableReader(&fakeRows{rows: rows})
	h := NewHandler(s)
	get := func(query string) listResponse {
		t.Helper()
		w := httptest.NewRecorder()
		h.HandleList(w, httptest.NewRequest("GET", "/api/v1/releases?"+query, nil))
		var out listResponse
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var seen []string
	page := get("lane=platform&limit=3")
	for i := 0; ; i++ {
		if page.Total != 7 || page.Error != "" {
			t.Fatalf("page %d: total %d, error %q", i, page.Total, page.Error)
		}
		for _, r := range page.Records {
			seen = append(seen, r.Run)
		}
		if page.Next == "" {
			break
		}
		page = get("lane=platform&limit=3&before=" + page.Next)
	}
	want := "[run-6 run-5 run-4 run-3 run-2 run-1 run-0]"
	if fmt.Sprint(seen) != want {
		t.Fatalf("paged %v, want %s", seen, want)
	}
	if got := get("lane=platform&before=nowhere"); got.Error == "" || len(got.Records) != 0 {
		t.Fatalf("an unknown before: %+v", got)
	}
}
