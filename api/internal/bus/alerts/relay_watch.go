package alerts

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const relayWatchEvery = time.Minute

// RelayStatusURL is the relay's status read, beside the notify address the
// platform already has; "" when it has none.
func RelayStatusURL() string {
	u, ok := strings.CutSuffix(strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL")), "/alerts/notify")
	if !ok {
		return ""
	}
	return u + "/alerts/relay"
}

// WebhookQuery is the Prometheus query for Alertmanager's webhook to the
// platform failing.
const WebhookQuery = `ALERTS{alertname="` + NameWebhookFailing + `",alertstate="firing"}`

// StartRelayWatch says on the bus what the phone relay knows: the Watchdog
// stopped arriving, or the relay cannot be reached (RelayWatch). db is the
// ledger, nil while it is not connected; webhook says what Prometheus says of
// Alertmanager's webhook, known false when it does not answer.
func StartRelayWatch(ctx context.Context, db func() eventlog.DB, webhook func(context.Context) (failing, known bool)) {
	url := RelayStatusURL()
	if url == "" {
		return
	}
	safego.Go("bus-alert-relay-watch", func() {
		client := &http.Client{Timeout: 10 * time.Second}
		var w RelayWatch
		seeded := false
		tick := time.NewTicker(relayWatchEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			d := db()
			if d == nil {
				continue
			}
			if !seeded {
				// What the ledger last said, so a restart neither repeats a
				// firing alert nor leaves one unresolved.
				if err := eventlog.InTx(ctx, d, func(tx pgx.Tx) error {
					var err error
					if w.Missing, err = firing(ctx, tx, NameHeartbeatMissing); err != nil {
						return err
					}
					if w.Unreachable, err = firing(ctx, tx, NameRelayUnreachable); err != nil {
						return err
					}
					w.Webhook, err = firing(ctx, tx, NameWebhookFailing)
					return err
				}); err != nil {
					slog.Warn("bus: alert relay watch not seeded", "error", err)
					continue
				}
				seeded = true
			}
			prev := w
			reading := readRelay(ctx, client, url)
			reading.WebhookFailing, reading.WebhookKnown = webhook(ctx)
			if list := w.Step(reading); len(list) > 0 {
				if err := Announce(ctx, d, list); err != nil {
					slog.Warn("bus: alert relay change not said", "error", err)
					w = prev // said on the next look
				}
			}
		}
	})
}

func firing(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	r, ok, err := eventlog.Last(ctx, tx, Stream(name))
	if err != nil || !ok {
		return false, err
	}
	return r.Body["status"] == "firing", nil
}

func readRelay(ctx context.Context, client *http.Client, url string) RelayReading {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RelayReading{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return RelayReading{}
	}
	defer resp.Body.Close()
	var st struct {
		Enabled     bool `json:"enabled"`
		HeartbeatOK bool `json:"heartbeat_ok"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&st) != nil {
		return RelayReading{}
	}
	return RelayReading{Reached: true, Enabled: st.Enabled, HeartbeatOK: st.HeartbeatOK}
}
