// Package relay is the bus's relay (api/internal/bus/RULES.md §7): it reads
// the ledger (agentrt's event log) after its cursor, for the registered event
// types, and publishes each event to NATS JetStream as a CloudEvents envelope
// (§2) that points at the state and carries none of it. It also holds what
// the gate and the resident subscribers share with it: the envelope, the
// cursor, the ledger reading and the NATS streams.
//
// It imports no domain package: the ledger's cross-stream read
// (eventlog.EventsAfter) is handed in by the server's wiring.
package relay

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Tenant is the platform's own tenant (RULES.md §2).
const Tenant = "rocket"

// MaxEnvelope is the largest envelope the bus carries (RULES.md §2).
const MaxEnvelope = 2048

// Cursor is a position in the ledger, "<txid>.<event_id>": every event of a
// transaction below TxID, and of TxID up to EventID, comes before it. It is
// the event log's read-on cursor (eventlog.EventCursor), the same text.
type Cursor struct{ TxID, EventID int64 }

func (c Cursor) String() string {
	return strconv.FormatInt(c.TxID, 10) + "." + strconv.FormatInt(c.EventID, 10)
}

// Less orders cursors as the ledger does.
func (c Cursor) Less(o Cursor) bool {
	return c.TxID < o.TxID || (c.TxID == o.TxID && c.EventID < o.EventID)
}

// IsZero is the cursor before everything.
func (c Cursor) IsZero() bool { return c == Cursor{} }

// ParseCursor reads "<txid>.<event_id>".
func ParseCursor(s string) (Cursor, bool) {
	t, e, found := strings.Cut(strings.TrimSpace(s), ".")
	txid, err1 := strconv.ParseInt(t, 10, 64)
	event, err2 := strconv.ParseInt(e, 10, 64)
	if !found || err1 != nil || err2 != nil || txid < 0 || event < 0 {
		return Cursor{}, false
	}
	return Cursor{txid, event}, true
}

// Row is one event of the ledger, as the relay and the gate read it.
type Row struct {
	EventID int64
	TxID    int64
	Stream  string
	Type    string
	Actor   string
	Cause   int64
	Body    map[string]any
	At      time.Time
}

// Cursor is the row's position.
func (r Row) Cursor() Cursor { return Cursor{r.TxID, r.EventID} }

// EventID is the envelope id of a ledger event: the ledger's event id, which
// is unique in an environment and never reused (the ledger has no ULID).
func EventID(eventID int64) string { return "ev-" + strconv.FormatInt(eventID, 10) }

var eventIDRE = regexp.MustCompile(`^ev-([1-9][0-9]{0,18})$`)

// ParseEventID is the ledger event id of an envelope id.
func ParseEventID(id string) (int64, bool) {
	m := eventIDRE.FindStringSubmatch(id)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return n, err == nil
}

// Envelope is the CloudEvents 1.0 structured JSON of one event (RULES.md §2):
// the standard attributes and the bus's extensions, never data.
type Envelope struct {
	SpecVersion string `json:"specversion"`
	ID          string `json:"id"`
	Source      string `json:"source"`
	Type        string `json:"type"`
	Time        string `json:"time"`
	Subject     string `json:"subject"`
	Tenant      string `json:"tenant"`
	Env         string `json:"env"`
	Cursor      string `json:"cursor"`
	Ref         string `json:"ref"`
	Actor       string `json:"actor"`
	Key         string `json:"key,omitempty"`
	Change      string `json:"change,omitempty"`
	CauseID     string `json:"causeid,omitempty"`
	// OnBehalfOf is whom the event tells of an act for (a thread or seat
	// address): the seat that asked for a run (2026-11-17), from the body's
	// on_behalf_of. The actor stays who wrote the event.
	OnBehalfOf string `json:"onbehalfof,omitempty"`
}

// EnvRE is what an environment name may be: it is a segment of NATS subjects.
var EnvRE = regexp.MustCompile(`^[a-z0-9]+$`)

// Build makes the envelope of a ledger row of a registered type, and its
// JSON. The subject, ref, key and change are the body's fixed fields
// (RULES.md §7); an event of a type whose register entry names a key must
// carry it. An envelope over MaxEnvelope is refused.
func Build(row Row, ch registry.Channel, tenant, env string) (Envelope, []byte, error) {
	str := func(name string) string {
		s, _ := row.Body[name].(string)
		return s
	}
	e := Envelope{
		SpecVersion: "1.0",
		ID:          EventID(row.EventID),
		Source:      "bifrost://" + tenant + "/" + env + "/" + ch.Publisher,
		Type:        row.Type,
		Time:        row.At.UTC().Format(time.RFC3339Nano),
		Subject:     str("subject"),
		Tenant:      tenant,
		Env:         env,
		Cursor:      row.Cursor().String(),
		Ref:         str("ref"),
		Actor:       row.Actor,
		Key:         str("key"),
		Change:      str("change"),
		OnBehalfOf:  str("on_behalf_of"),
	}
	if row.Cause != 0 {
		e.CauseID = EventID(row.Cause)
	}
	switch {
	case ch.Type != row.Type:
		return e, nil, fmt.Errorf("bus: event %d is a %s, not a %s", row.EventID, row.Type, ch.Type)
	case !EnvRE.MatchString(env):
		return e, nil, fmt.Errorf("bus: environment %q is not a subject segment", env)
	case e.Subject == "":
		return e, nil, fmt.Errorf("bus: event %d (%s) has no subject", row.EventID, row.Type)
	case !strings.HasPrefix(e.Ref, "/api/v1/"):
		return e, nil, fmt.Errorf("bus: event %d (%s) has no ref to a platform read", row.EventID, row.Type)
	case ch.Key != "" && e.Key == "":
		return e, nil, fmt.Errorf("bus: event %d (%s) has no key", row.EventID, row.Type)
	case e.Actor == "":
		return e, nil, fmt.Errorf("bus: event %d (%s) has no actor", row.EventID, row.Type)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return e, nil, err
	}
	if len(data) > MaxEnvelope {
		return e, nil, fmt.Errorf("bus: the envelope of event %d is %d bytes, over %d", row.EventID, len(data), MaxEnvelope)
	}
	return e, data, nil
}

// EventSubject is the NATS subject of an event type in env (RULES.md §3).
func EventSubject(tenant, env, typ string) string { return "ev." + tenant + "." + env + "." + typ }

// EventStreamName is the JetStream stream of a tenant's events in env (§11).
func EventStreamName(tenant, env string) string {
	return "EV_" + strings.ToUpper(tenant) + "_" + strings.ToUpper(env)
}

// ReceiptSubject is where every delivery receipt of an environment is
// published: one subject; the event id and the consumer ride in headers, so
// no subject name is made of an id (RULES.md §2).
func ReceiptSubject(tenant, env string) string { return "rcpt." + tenant + "." + env }

// The headers of a receipt.
const (
	ReceiptEventHeader    = "Bifrost-Event-Id"
	ReceiptConsumerHeader = "Bifrost-Consumer"
)

// ReceiptStreamName is the JetStream stream of a tenant's receipts in env.
func ReceiptStreamName(tenant, env string) string {
	return "RCPT_" + strings.ToUpper(tenant) + "_" + strings.ToUpper(env)
}

// CursorHeader is the header each published message carries its ledger
// cursor in; the relay resumes from the last message's.
const CursorHeader = "Bifrost-Cursor"

// Receipt is the body of a delivery receipt.
type Receipt struct {
	Consumer string `json:"consumer"`
	At       string `json:"at"`
}
