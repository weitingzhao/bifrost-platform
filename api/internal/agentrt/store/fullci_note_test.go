package store

import "testing"

func TestFullCIRedRecordsAlertAndDeviation(t *testing.T) {
	t.Parallel()
	b := (FullCI{Run: "daily-red", Commit: "main"}).body()
	if b["alert"] != true || b["deviation"] != "content_reuse" {
		t.Fatalf("red audit lacks alert/deviation: %v", b)
	}
	if _, ok := (FullCI{Started: []string{"daily"}}).body()["alert"]; ok {
		t.Fatal("starting an audit raised an alert")
	}
}
