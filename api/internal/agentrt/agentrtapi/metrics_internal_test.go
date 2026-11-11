package agentrtapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMetricQueryLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query string
		limit int
		valid bool
	}{
		{"", 0, false}, {"?metric=x", 30, true}, {"?metric=x&limit=500", 500, true},
		{"?metric=x&limit=0", 0, false}, {"?metric=x&limit=501", 0, false}, {"?metric=x&limit=no", 0, false},
	} {
		p, err := parseMetricQuery(httptest.NewRequest("GET", "/"+tc.query, nil))
		if (err == nil) != tc.valid || (tc.valid && p.limit != tc.limit) {
			t.Fatalf("%s: %+v, %v", tc.query, p, err)
		}
	}
}

func TestMetricComparisonEvidence(t *testing.T) {
	t.Parallel()
	x, y := 12.25, 15.5
	b := comparisonReading{Version: 1, SHA: "a", Unit: "seconds", Value: &x, Conclusive: true}
	c := b
	c.Value = &y
	same, conclusive, change := metricChange(b, c)
	if !same || !conclusive || change == nil || *change != 3.25 {
		t.Fatal("missing difference")
	}
	c.Conclusive = false
	same, conclusive, change = metricChange(b, c)
	if !same || conclusive || change != nil {
		t.Fatal("conclusive without evidence")
	}
	c = b
	c.SHA = "b"
	same, conclusive, change = metricChange(b, c)
	if same || conclusive || change != nil {
		t.Fatal("compared different definitions")
	}
	c = b
	c.Value = nil
	_, conclusive, change = metricChange(b, c)
	if conclusive || change != nil {
		t.Fatal("compared missing sample")
	}
	doc, err := enforceMetricFloor([]byte(`{"metric_id":"lead_time_s","definition_version":1,"samples":0,"value":null,"conclusive":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Conclusive bool `json:"conclusive"`
	}
	if err = json.Unmarshal(doc, &out); err != nil || out.Conclusive {
		t.Fatalf("sample floor: %s, %v", doc, err)
	}
}
