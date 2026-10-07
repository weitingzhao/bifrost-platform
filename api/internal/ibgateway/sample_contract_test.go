package ibgateway

import "testing"

func TestSampleContractFollowsGatewayConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]string
		want string
	}{
		{"explicit key wins", map[string]string{"sample_contract": "SPY|STK|||", "gateway.yaml": "watchlist_symbols: [AAA]"}, "SPY|STK|||"},
		{"first watchlist symbol", map[string]string{"gateway.yaml": "mode: live\nwatchlist_symbols: [aaa, BBB]\n"}, "AAA|STK|||"},
		{"nothing configured", map[string]string{"mode": "live"}, ""},
		{"unparseable yaml", map[string]string{"gateway.yaml": "watchlist_symbols: ["}, ""},
	} {
		if got := sampleContractFromConfig(tc.data); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
