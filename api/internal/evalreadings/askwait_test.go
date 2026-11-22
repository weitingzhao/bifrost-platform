package evalreadings

import (
	"testing"
	"time"
)

// 2026-11-22, the communication line: seat.ask.overdue's return must be
// measured before and after it ships by the Bus page's rule. An ask answered
// in the window counts toward the median and the longest; one sent the day
// before and answered today counts for neither; an unanswered ask counts as
// open only past 4 hours at the window's end.
func TestAskWait(t *testing.T) {
	day := ClosedWindow(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC), 1)
	at := func(d time.Duration) time.Time { return day.From.Add(d) }
	ptr := func(t time.Time) *time.Time { return &t }
	var rows []askRow
	for i := 0; i < 10; i++ { // ten asks answered in 10 minutes each
		rows = append(rows, askRow{At: at(time.Duration(i) * time.Hour), Replied: ptr(at(time.Duration(i)*time.Hour + 10*time.Minute))})
	}
	rows = append(rows,
		askRow{At: at(time.Hour), Replied: ptr(at(7 * time.Hour))},  // 6 hours: the longest
		askRow{At: at(-2 * time.Hour), Replied: ptr(at(time.Hour))}, // sent the day before
		askRow{At: day.To.Add(-5 * time.Hour)},                      // open, past 4 hours
		askRow{At: day.To.Add(-time.Hour)},                          // open, under 4 hours
		askRow{At: at(-3 * 24 * time.Hour)},                         // open for days
	)
	src := ledgerSource{asks: func(from, to time.Time) ([]askRow, error) {
		if !to.Equal(day.To) || !from.Equal(day.To.Add(-askLookBack)) {
			t.Fatalf("read %s..%s", from, to)
		}
		return rows, nil
	}}
	rs, err := src.askWaitReadings(day)
	if err != nil || len(rs) != 3 {
		t.Fatalf("%v %+v", err, rs)
	}
	if rs[0].Name != "ask_answer_p50_seconds" || rs[0].Status != StatusOK || *rs[0].Value != 600 || rs[0].Samples != 11 {
		t.Fatalf("median: %+v", rs[0])
	}
	if *rs[1].Value != 6*3600 {
		t.Fatalf("longest: %+v", rs[1])
	}
	if *rs[2].Value != 2 {
		t.Fatalf("open past 4 hours: %+v", rs[2])
	}
	if rs, _ := (ledgerSource{}).askWaitReadings(day); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no reader must be unmeasured: %+v", rs[0])
	}
}
