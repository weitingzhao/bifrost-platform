package effect

import (
	"testing"
	"time"
)

// 2026-11-17 (the Owner cannot see what the bus's work does): "main moved →
// STG delivery started" pairs each platform landing with the first delivery
// created at or after it, within 30 minutes, inside the window; this is the
// same pairing the 51-second baseline of 2026-11-16 was measured with.
func TestLatenciesPairLandingWithNextDelivery(t *testing.T) {
	t0 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	done := []time.Time{at(0), at(1000), at(5000), at(-90000)}
	created := []time.Time{at(11), at(1300), at(9000)} // 11 s; 300 s; the third landing has none within 30 min
	m, n, fast := latencies(done, created, at(-3600), at(86400))
	if n != 2 || fast != 1 || m != 155.5 {
		t.Fatalf("median %v, %d paired, %d fast", m, n, fast)
	}
	if _, n, _ := latencies(done, nil, at(-3600), at(86400)); n != 0 {
		t.Fatalf("paired without deliveries: %d", n)
	}
}
