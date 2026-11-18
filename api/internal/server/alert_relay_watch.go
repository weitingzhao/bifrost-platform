package server

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
	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/nextgen"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const alertRelayWatchEvery = time.Minute

// alertRelayStatusURL is the relay's status read, beside the notify address the
// platform already has; "" when it has none.
func alertRelayStatusURL() string {
	u, ok := strings.CutSuffix(strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL")), "/alerts/notify")
	if !ok {
		return ""
	}
	return u + "/alerts/relay"
}

// startAlertRelayWatch says on the bus what the phone relay knows: the Watchdog
// stopped arriving, or the relay cannot be reached (bus/alerts.RelayWatch).
func (s *Server) startAlertRelayWatch(ctx context.Context) {
	url := alertRelayStatusURL()
	if url == "" {
		return
	}
	safego.Go("bus-alert-relay-watch", func() {
		client := &http.Client{Timeout: 10 * time.Second}
		var w busalerts.RelayWatch
		seeded := false
		tick := time.NewTicker(alertRelayWatchEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			db := s.bus.db()
			if db == nil {
				continue
			}
			if !seeded {
				// What the ledger last said, so a restart neither repeats a
				// firing alert nor leaves one unresolved.
				if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
					var err error
					if w.Missing, err = alertFiring(ctx, tx, busalerts.NameHeartbeatMissing); err != nil {
						return err
					}
					w.Unreachable, err = alertFiring(ctx, tx, busalerts.NameRelayUnreachable)
					return err
				}); err != nil {
					slog.Warn("bus: alert relay watch not seeded", "error", err)
					continue
				}
				seeded = true
			}
			prev := w
			if list := w.Step(readAlertRelay(ctx, client, url)); len(list) > 0 {
				if err := busalerts.Announce(ctx, db, list); err != nil {
					slog.Warn("bus: alert relay change not said", "error", err)
					w = prev // said on the next look
				}
			}
		}
	})
}

func alertFiring(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	r, ok, err := eventlog.Last(ctx, tx, busalerts.Stream(name))
	if err != nil || !ok {
		return false, err
	}
	return r.Body["status"] == "firing", nil
}

func readAlertRelay(ctx context.Context, client *http.Client, url string) busalerts.RelayReading {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return busalerts.RelayReading{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return busalerts.RelayReading{}
	}
	defer resp.Body.Close()
	var st struct {
		Enabled     bool `json:"enabled"`
		HeartbeatOK bool `json:"heartbeat_ok"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&st) != nil {
		return busalerts.RelayReading{}
	}
	return busalerts.RelayReading{Reached: true, Enabled: st.Enabled, HeartbeatOK: st.HeartbeatOK}
}

// problemEvidence is evidence for the problem channels. Some topics are shared,
// and their traffic says nothing about one channel, so a channel can name its
// own proof: "alert:<name>" and "stream:<id>" are the last event of a stream,
// "source:<type>:<source>" the last event of that type from that source.
// Anything else goes to base.
func (s *Server) problemEvidence(ctx context.Context, now time.Time, base func(string) nextgen.Evidence) func(string) nextgen.Evidence {
	return func(id string) nextgen.Evidence {
		kind, rest, _ := strings.Cut(id, ":")
		var read func(tx pgx.Tx) (eventlog.Row, bool, error)
		switch kind {
		case "alert", "stream":
			stream := rest
			if kind == "alert" {
				stream = busalerts.Stream(rest)
			}
			read = func(tx pgx.Tx) (eventlog.Row, bool, error) { return eventlog.Last(ctx, tx, stream) }
		case "source":
			typ, source, _ := strings.Cut(rest, ":")
			read = func(tx pgx.Tx) (eventlog.Row, bool, error) {
				return eventlog.LatestWithField(ctx, tx, typ, "source", source, now.Add(-nextgen.EvidenceWindow))
			}
		default:
			return base(id)
		}
		db := s.bus.db()
		if db == nil {
			return nextgen.Evidence{State: nextgen.EvidenceUnmeasured}
		}
		var last float64
		err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			r, found, err := read(tx)
			if found {
				last = float64(r.At.Unix())
			}
			return err
		})
		if err != nil {
			return nextgen.Evidence{State: nextgen.EvidenceUnmeasured}
		}
		return nextgen.JudgeEvidence(now, last, 1)
	}
}
