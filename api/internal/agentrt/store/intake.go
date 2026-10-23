package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Intake clients and the people they speak for (DESIGN-crew-intake §5.2,
// §5.5; migrations 0009): each external agent is an actor agent:<name>, its
// token intake:<name>, bound to at most one person and to its products, with
// daily limits. Only the Owner sets them up (intake_client_set); a client
// acknowledges its own updates (intake_acked); how far it was served moves
// without an event (the liveness pair, R2-A5).

// IntakeStream is the stream of a client's setup and acknowledgements;
// PeopleStream that of the people.
func IntakeStream(client string) string { return "intake:" + client }

// PeopleStream is the event stream of the people intake serves.
const PeopleStream = "intake:people"

// IntakeTokenName is the token an intake client holds (§5.2).
func IntakeTokenName(client string) string { return "intake:" + strings.TrimPrefix(client, "agent:") }

// intakeClient is a client's row as the checks read it.
type intakeClient struct {
	id, token, behalfOf      string
	products                 []string
	submits, adds, proposals int
	revoked                  bool
	served, acked            EventCursor
	hasServed, hasAcked      bool
}

func readIntakeClient(ctx context.Context, tx pgx.Tx, id string, lock bool) (intakeClient, error) {
	q := `SELECT intake_client_id, token_name, COALESCE(on_behalf_of, ''), products, daily_submits, daily_adds, daily_proposals,
	  revoked_at IS NOT NULL, COALESCE(served_txid, 0), COALESCE(served_event, 0), served_txid IS NOT NULL,
	  COALESCE(acked_txid, 0), COALESCE(acked_event, 0), acked_txid IS NOT NULL
	  FROM agentrt.intake_client WHERE intake_client_id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	var c intakeClient
	err := tx.QueryRow(ctx, q, id).Scan(&c.id, &c.token, &c.behalfOf, &c.products, &c.submits, &c.adds, &c.proposals, &c.revoked,
		&c.served.TxID, &c.served.EventID, &c.hasServed, &c.acked.TxID, &c.acked.EventID, &c.hasAcked)
	return c, err
}

// holdsIntakeTx checks an external agent's writer, for a read, a write and
// before a replay of its key (§2.1): it has a client row, its token is the
// one bound to it, and it is not revoked (revoked_at takes effect at once).
func holdsIntakeTx(ctx context.Context, tx pgx.Tx, w Writer) error {
	c, err := readIntakeClient(ctx, tx, w.Actor, false)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %s is no intake client", ErrNotYours, w.Actor)
	case err != nil:
		return err
	case c.token != w.Token:
		return fmt.Errorf("%w: %s is bound to token %s, not %s", ErrNotYours, w.Actor, c.token, w.Token)
	case c.revoked:
		return fmt.Errorf("%w: %s", ErrRevoked, w.Actor)
	}
	return nil
}

// intakeSees says whether an external agent may read and add to an issue
// (§2.1, §5.3): of its products, and serving the person it is bound to, or
// its own proposal.
func intakeSees(ctx context.Context, tx pgx.Tx, client string, issue issueRow) error {
	c, err := readIntakeClient(ctx, tx, client, false)
	if err != nil {
		return err
	}
	mine := (issue.ServedPerson != "" && issue.ServedPerson == c.behalfOf) || (issue.ServedPerson == "" && issue.creator == client)
	if !slices.Contains(c.products, issue.Product) || !mine {
		// The same answer as an issue that does not exist: an agent learns
		// nothing of issues it does not see.
		return invalid("there is no issue %s", issue.ID)
	}
	return nil
}

// PutPerson sets up a person (§5.5): the Owner only. Its name stays in the
// database.
func PutPerson(ctx context.Context, db DB, w Writer, id, name string) (map[string]any, error) {
	if w.Actor != "owner" {
		return nil, invalid("only the Owner sets up the people intake serves")
	}
	if !personIDRE.MatchString(id) {
		return nil, invalid("%q is not a person (person:<name>)", id)
	}
	if err := checkText("a person's name", name, 200); err != nil {
		return nil, err
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		if _, err := Append(ctx, tx, Event{Stream: PeopleStream, Type: "intake_client_set", Actor: w.Actor,
			Body: map[string]any{"change": "person", "person_id": id}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.person (person_id, display_name) VALUES ($1, $2)
		  ON CONFLICT (person_id) DO UPDATE SET display_name = EXCLUDED.display_name`, id, name)
		out = map[string]any{"person_id": id}
		return err
	})
	return out, err
}

// IntakeClientSetup is an external agent's setup (§5.5): whom it speaks for
// ("" = only its own proposals), its products and its daily limits (nil =
// the default: 10 submissions, 20 additions, 3 proposals).
type IntakeClientSetup struct {
	ClientID, OnBehalfOf     string
	Products                 []string
	Submits, Adds, Proposals *int
}

// SetIntakeClient sets up or changes an intake client: the Owner only. A new
// client's actor is registered with it (actor_registered). A revoked client
// is never set up again (0009): a new name is.
func SetIntakeClient(ctx context.Context, db DB, w Writer, s IntakeClientSetup) (map[string]any, error) {
	if w.Actor != "owner" {
		return nil, invalid("only the Owner sets up intake clients")
	}
	if !agentIDRE.MatchString(s.ClientID) {
		return nil, invalid("%q is not an intake client (agent:<name>)", s.ClientID)
	}
	if s.OnBehalfOf != "" && !personIDRE.MatchString(s.OnBehalfOf) {
		return nil, invalid("%q is not a person", s.OnBehalfOf)
	}
	if len(s.Products) == 0 {
		return nil, invalid("an intake client brings issues of at least one product")
	}
	for _, p := range s.Products {
		if !productRE.MatchString(p) {
			return nil, invalid("%q is not a product key", p)
		}
	}
	limits := []int{10, 20, 3}
	for i, p := range []*int{s.Submits, s.Adds, s.Proposals} {
		if p != nil {
			if *p < 0 {
				return nil, invalid("a daily limit is not negative")
			}
			limits[i] = *p
		}
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		var kind string
		err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1`, s.ClientID).Scan(&kind)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := Append(ctx, tx, Event{Stream: "system", Type: "actor_registered", Actor: w.Actor,
				Body: map[string]any{"actors": []string{s.ClientID}, "note": "an intake client, set up by the Owner"}}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'agent', $2)`,
				s.ClientID, "Intake client "+strings.TrimPrefix(s.ClientID, "agent:")); err != nil {
				return err
			}
		case err != nil:
			return err
		case kind != "agent":
			return invalid("%s is a %s, not an agent", s.ClientID, kind)
		}
		if _, err := Append(ctx, tx, Event{Stream: IntakeStream(s.ClientID), Type: "intake_client_set", Actor: w.Actor,
			Body: map[string]any{"change": "client", "intake_client_id": s.ClientID}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.intake_client (intake_client_id, token_name, on_behalf_of, products,
		    daily_submits, daily_adds, daily_proposals) VALUES ($1, $2, $3, $4, $5, $6, $7)
		  ON CONFLICT (intake_client_id) DO UPDATE SET on_behalf_of = EXCLUDED.on_behalf_of, products = EXCLUDED.products,
		    daily_submits = EXCLUDED.daily_submits, daily_adds = EXCLUDED.daily_adds, daily_proposals = EXCLUDED.daily_proposals`,
			s.ClientID, IntakeTokenName(s.ClientID), nullable(s.OnBehalfOf), s.Products, limits[0], limits[1], limits[2])
		out = map[string]any{"intake_client_id": s.ClientID, "token_name": IntakeTokenName(s.ClientID)}
		return err
	})
	return out, err
}

// RevokeIntakeClient revokes a client at once (§5.2, §5.4): every read,
// write and replay after it is refused.
func RevokeIntakeClient(ctx context.Context, db DB, w Writer, client string) error {
	if w.Actor != "owner" {
		return invalid("only the Owner revokes an intake client")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		c, err := readIntakeClient(ctx, tx, client, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("%s is no intake client", client)
		}
		if err != nil || c.revoked {
			return err
		}
		if _, err := Append(ctx, tx, Event{Stream: IntakeStream(client), Type: "intake_client_set", Actor: w.Actor,
			Body: map[string]any{"change": "revoke", "intake_client_id": client}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.intake_client SET revoked_at = clock_timestamp() WHERE intake_client_id = $1`, client)
		return err
	})
}

// MarkServed moves how far a client was served to c, never back: the
// largest cursor a read of its updates gave it, written with no event (the
// liveness pair, R2-A5). A cursor not past the stored one changes nothing.
func MarkServed(ctx context.Context, tx pgx.Tx, client string, c EventCursor) error {
	_, err := tx.Exec(ctx, `UPDATE agentrt.intake_client SET served_txid = $2, served_event = $3
	  WHERE intake_client_id = $1 AND (served_txid IS NULL OR (served_txid, served_event) < ($2::bigint, $3::bigint))`,
		client, c.TxID, c.EventID)
	return err
}

// AckIntake records that the client has stored its updates up to c (§2.1,
// B3): forward only, never past what it was served; an older cursor is
// ignored, without error. It answers where its acknowledgement stands.
func AckIntake(ctx context.Context, db DB, w Writer, c EventCursor) (map[string]any, error) {
	if kindOf(w.Actor) != "agent" {
		return nil, invalid("an intake client acknowledges its own updates")
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		cl, err := readIntakeClient(ctx, tx, w.Actor, true)
		if err != nil {
			return err
		}
		switch {
		case !cl.hasServed || cl.served.Less(c):
			return invalid("%s was served up to %s; it acknowledges no further", w.Actor, servedText(cl))
		case cl.hasAcked && !cl.acked.Less(c):
			out = map[string]any{"acked": cl.acked.String(), "moved": false}
			return nil
		}
		if _, err := Append(ctx, tx, Event{Stream: IntakeStream(w.Actor), Type: "intake_acked", Actor: w.Actor,
			Body: map[string]any{"intake_client_id": w.Actor, "cursor": c.String()}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.intake_client SET acked_txid = $2, acked_event = $3, acked_at = clock_timestamp()
		  WHERE intake_client_id = $1`, w.Actor, c.TxID, c.EventID)
		out = map[string]any{"acked": c.String(), "moved": true}
		return err
	})
	return out, err
}

func servedText(c intakeClient) string {
	if !c.hasServed {
		return "nothing"
	}
	return c.served.String()
}

// intakeDaily checks an external agent's daily limits before it submits or
// adds (§2.1: 10 submissions, 20 additions, 3 of its own proposals a day by
// default; 429): counted from its own events of the last 24 hours, under its
// writer's lock, so two of its calls cannot both take the last one. An own
// proposal counts against both the submissions and the proposals.
func intakeDaily(ctx context.Context, tx pgx.Tx, client, eventType string, proposal bool) error {
	c, err := readIntakeClient(ctx, tx, client, false)
	if err != nil {
		return err
	}
	var n, own int
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT body_json ? 'served_person') FROM agentrt.event
	  WHERE actor_id = $1 AND event_type = $2 AND at > clock_timestamp() - interval '24 hours'`, client, eventType).Scan(&n, &own); err != nil {
		return err
	}
	limit := c.adds
	if eventType == "issue_submitted" {
		limit = c.submits
		if proposal && own >= c.proposals {
			return fmt.Errorf("%w: %s brought %d of its own proposals in the last day, its limit", ErrQuota, client, own)
		}
	}
	if n >= limit {
		return fmt.Errorf("%w: %s wrote %d %s in the last day, its limit", ErrQuota, client, n, eventType)
	}
	return nil
}
