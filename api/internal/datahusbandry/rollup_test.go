package datahusbandry

import "testing"

func TestRollupResearchOLAP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		product, pDet   string
		batch, bDet     string
		wantVerdict     string
		wantDetailSubstr string
	}{
		{
			name:        "both healthy",
			product:     "healthy",
			pDet:        "signal ok",
			batch:       "healthy",
			bDet:        "dagster ok",
			wantVerdict: "healthy",
		},
		{
			name:             "product ok batch unknown → caution",
			product:          "healthy",
			pDet:             "signal ok",
			batch:            "unknown",
			bDet:             "orchestration unprobed",
			wantVerdict:      "caution",
			wantDetailSubstr: "product ok",
		},
		{
			name:        "batch overdue beats product healthy",
			product:     "healthy",
			pDet:        "signal ok",
			batch:       "missed",
			bDet:        "overdue",
			wantVerdict: "missed",
		},
		{
			name:        "product degraded with healthy batch",
			product:     "degraded",
			pDet:        "stale",
			batch:       "healthy",
			bDet:        "ok",
			wantVerdict: "degraded",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, detail := rollupResearchOLAP(tc.product, tc.pDet, tc.batch, tc.bDet)
			if got != tc.wantVerdict {
				t.Fatalf("verdict=%q want %q (detail=%q)", got, tc.wantVerdict, detail)
			}
			if tc.wantDetailSubstr != "" && !contains(detail, tc.wantDetailSubstr) {
				t.Fatalf("detail=%q missing %q", detail, tc.wantDetailSubstr)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
