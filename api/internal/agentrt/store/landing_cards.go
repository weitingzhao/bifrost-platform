package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Landing cards (S3 B3, CUT §11.6 v2/v3): the Owner's cards about landings,
// kept on a stream of their own, landing-cards:<repo>, apart from the
// repository's landing stream, so making a card (an external Ask) never holds
// the stream the landing gates lock. Five records, each a note written by the
// controller:
//
//	landing_card_prepared     the card to ask: its trigger, number, the frozen
//	                          request and the letter → choice map
//	landing_card_ask_started  an Ask may have reached the question store
//	landing_card              the question the store gave it (one per prep)
//	landing_card_acked        its answer was handled and acknowledged
//	landing_card_closed       it ended otherwise (expired, withdrawn, lost…)
//
// Only these records say what a card is; the question store is asked, never
// trusted to remember (it trims answered questions after seven days).

// CardsStream is the card stream of a repository.
func CardsStream(repo string) string { return "landing-cards:" + repo }

// Card kinds and the choices each offers (CUT §11.6 v2 item 1, v3 item 4):
// every card has two options; the second of a one-decision card defers it.
const (
	CardRootFailed       = "root_failed"
	CardFloor            = "floor"
	CardOkMissing        = "ok_missing"
	CardLateLanding      = "late_landing"
	CardRepoStopped      = "repo_stopped"
	CardCIStuck          = "ci_stuck"
	CardParkedLong       = "parked_long"
	CardLandedUnverified = "landed_unverified"
	CardInMain           = "in_main"
)

// cardChoices are the semantic choices each kind may offer, in order.
var cardChoices = map[string][][]string{
	CardRootFailed:       {{"try_again", "do_not_merge"}, {"do_not_merge", "wait"}},
	CardFloor:            {{"do_not_merge", "wait"}},
	CardOkMissing:        {{"confirm_main_lost", "wait"}},
	CardLateLanding:      {{"ack", "remind_later"}},
	CardRepoStopped:      {{"repo_resume", "keep_stopped"}},
	CardCIStuck:          {{"ack", "remind_later"}},
	CardParkedLong:       {{"ack", "remind_later"}},
	CardLandedUnverified: {{"confirm_not_delivered", "wait"}},
	CardInMain:           {{"count_as_delivered", "confirm_not_delivered"}},
}

// repoCards are the kinds about the repository, not a root.
var repoCards = []string{CardRepoStopped}

// Why a card ended without an acknowledged answer (CUT §11.6 v3 item 2).
var cardClosedWhys = []string{"unconfirmed", "expired", "withdrawn", "gone_after_handled", "lost", "resolved"}

var cardDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CardPrepID names a card by its trigger, number and kind: the same card is
// the same id on every pass and every process.
func CardPrepID(kind string, triggerSeq int64, n int) string {
	return fmt.Sprintf("card:%d:%d:%s", triggerSeq, n, kind)
}

// cardRecordKeys are the note keys of the card records.
var cardRecordKeys = []string{"landing_card_prepared", "landing_card_ask_started", "landing_card", "landing_card_acked", "landing_card_closed"}

func cardNumber(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// badCardRecord is why a card record's body is refused, or "".
func badCardRecord(name string, v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return name + " is not an object"
	}
	if m["v"] != "1" {
		return name + " version is not 1"
	}
	prep, _ := m["prep"].(string)
	if !strings.HasPrefix(prep, "card:") {
		return name + " needs prep"
	}
	switch name {
	case "landing_card_prepared":
		kind, _ := m["kind"].(string)
		sets, known := cardChoices[kind]
		if !known {
			return "landing_card_prepared kind is not a card kind"
		}
		seq, okSeq := cardNumber(m["trigger_seq"])
		n, okN := cardNumber(m["n"])
		tv, okT := cardNumber(m["template_version"])
		if !okSeq || seq < 1 || !okN || n < 1 || !okT || tv < 1 {
			return "landing_card_prepared needs trigger_seq, n and template_version"
		}
		if prep != CardPrepID(kind, seq, int(n)) {
			return "landing_card_prepared prep does not name its trigger, number and kind"
		}
		if repo, _ := m["repo"].(string); repo == "" {
			return "landing_card_prepared needs repo"
		}
		for _, k := range []string{"key", "request_fp"} {
			if s, _ := m[k].(string); !cardDigest.MatchString(s) {
				return "landing_card_prepared " + k + " is not a digest"
			}
		}
		if _, ok := m["request"].(map[string]any); !ok {
			return "landing_card_prepared needs request"
		}
		opts, ok := m["options"].(map[string]any)
		if !ok {
			return "landing_card_prepared needs options"
		}
		var choices []string
		for _, letter := range []string{"a", "b", "c", "d"} {
			if c, present := opts[letter]; present {
				s, _ := c.(string)
				choices = append(choices, s)
			}
		}
		if len(choices) != len(opts) || !slices.ContainsFunc(sets, func(set []string) bool { return slices.Equal(set, choices) }) {
			return "landing_card_prepared options are not the kind's choices in order"
		}
		gen, _ := cardNumber(m["stop_generation"])
		root, _ := m["root"].(string)
		op, _ := m["op"].(string)
		if slices.Contains(repoCards, kind) {
			if gen < 1 || root != "" || op != "" {
				return "landing_card_prepared of the repository names a stop generation and no root"
			}
		} else if gen != 0 || !landingRoot.MatchString(root) || op == "" {
			return "landing_card_prepared of a root names the root and its operation"
		}
	case "landing_card_ask_started":
	case "landing_card":
		if q, _ := m["question_id"].(string); q == "" {
			return "landing_card needs question_id"
		}
		if s, _ := m["created_at"].(string); s != "" {
			if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
				return "landing_card created_at is not a time"
			}
		}
	case "landing_card_acked":
		if q, _ := m["question_id"].(string); q == "" {
			return "landing_card_acked needs question_id"
		}
	case "landing_card_closed":
		if !slices.Contains(cardClosedWhys, fmt.Sprint(m["why"])) {
			return "landing_card_closed why is not a closing reason"
		}
	}
	return ""
}

// CardRecord is one card as its records say.
type CardRecord struct {
	Prep, Repo, Kind, Root, Op, Key, RequestFP, QuestionID, ClosedWhy string
	TriggerSeq, StopGeneration                                        int64
	N, TemplateVersion                                                int
	Request                                                           map[string]any
	Options                                                           map[string]string // letter → choice
	PreparedAt, AskStartedAt, BoundAt, CreatedAt                      time.Time
	AskStarted, Bound, Acked, Closed                                  bool
}

// Ended: acknowledged or closed; nothing more to do for this card.
func (c CardRecord) Ended() bool { return c.Acked || c.Closed }

// checkCardAppend: a card record is the controller's note on a card stream,
// alone in its body, and follows the card's earlier records (under the
// stream's row lock Append holds): one prepared per prep; ask_started and the
// binding after it, each once; one binding per question; acked after the
// binding; nothing after an end.
func checkCardAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	var name string
	var body map[string]any
	for _, k := range cardRecordKeys {
		if v, ok := ev.Body[k]; ok {
			if name != "" {
				return fmt.Errorf("one card record per note")
			}
			name, body = k, v.(map[string]any)
		}
	}
	if name == "" {
		return nil
	}
	if ev.Type != "note" || ev.Actor != "controller" || !strings.HasPrefix(ev.Stream, "landing-cards:") {
		return fmt.Errorf("%s is written by the controller on a card stream", name)
	}
	if _, ok := ev.Body["landing"]; ok {
		return fmt.Errorf("%s is not a landing record", name)
	}
	cards, err := LandingCards(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	prep := body["prep"].(string)
	c, exists := cards[prep]
	switch {
	case name == "landing_card_prepared":
		if exists {
			return fmt.Errorf("card %s is already prepared", prep)
		}
		if body["repo"] != strings.TrimPrefix(ev.Stream, "landing-cards:") {
			return fmt.Errorf("card %s is prepared on another repository's stream", prep)
		}
		return nil
	case !exists:
		return fmt.Errorf("card %s is not prepared", prep)
	case c.Ended():
		return fmt.Errorf("card %s has ended", prep)
	}
	switch name {
	case "landing_card_ask_started":
		if c.AskStarted {
			return fmt.Errorf("card %s ask already started", prep)
		}
	case "landing_card":
		if !c.AskStarted || c.Bound {
			return fmt.Errorf("card %s binds once, after its ask started", prep)
		}
		for _, o := range cards {
			if o.QuestionID == body["question_id"] {
				return fmt.Errorf("question %v is bound to %s already", body["question_id"], o.Prep)
			}
		}
	case "landing_card_acked":
		if !c.Bound || c.QuestionID != body["question_id"] {
			return fmt.Errorf("card %s acks its own bound question", prep)
		}
	case "landing_card_closed":
		if q, _ := body["question_id"].(string); q != "" && q != c.QuestionID {
			return fmt.Errorf("card %s closes its own question", prep)
		}
	}
	return nil
}

// LandingCards folds a card stream into its cards by prep.
func LandingCards(ctx context.Context, tx pgx.Tx, stream string) (map[string]CardRecord, error) {
	rows, err := tx.Query(ctx, `SELECT at, body_json FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' ORDER BY seq`, stream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]CardRecord{}
	for rows.Next() {
		var at time.Time
		var body map[string]any
		if err := rows.Scan(&at, &body); err != nil {
			return nil, err
		}
		for _, k := range cardRecordKeys {
			m, ok := body[k].(map[string]any)
			if !ok {
				continue
			}
			prep, _ := m["prep"].(string)
			c := out[prep]
			switch k {
			case "landing_card_prepared":
				c = CardRecord{Prep: prep, PreparedAt: at, Options: map[string]string{}}
				c.Repo, _ = m["repo"].(string)
				c.Kind, _ = m["kind"].(string)
				c.Root, _ = m["root"].(string)
				c.Op, _ = m["op"].(string)
				c.Key, _ = m["key"].(string)
				c.RequestFP, _ = m["request_fp"].(string)
				c.Request, _ = m["request"].(map[string]any)
				c.TriggerSeq, _ = cardNumber(m["trigger_seq"])
				c.StopGeneration, _ = cardNumber(m["stop_generation"])
				n, _ := cardNumber(m["n"])
				tv, _ := cardNumber(m["template_version"])
				c.N, c.TemplateVersion = int(n), int(tv)
				if opts, ok := m["options"].(map[string]any); ok {
					for letter, choice := range opts {
						c.Options[letter], _ = choice.(string)
					}
				}
			case "landing_card_ask_started":
				c.AskStarted, c.AskStartedAt = true, at
			case "landing_card":
				c.Bound, c.BoundAt = true, at
				c.QuestionID, _ = m["question_id"].(string)
				if s, _ := m["created_at"].(string); s != "" {
					c.CreatedAt, _ = time.Parse(time.RFC3339Nano, s)
				}
			case "landing_card_acked":
				c.Acked = true
			case "landing_card_closed":
				c.Closed = true
				c.ClosedWhy, _ = m["why"].(string)
			}
			out[prep] = c
		}
	}
	return out, rows.Err()
}

// appendCardRecord writes one card record on the repository's card stream.
func appendCardRecord(ctx context.Context, tx pgx.Tx, repo, name string, body map[string]any) error {
	body["v"] = "1"
	text := strings.ReplaceAll(strings.TrimPrefix(name, "landing_"), "_", " ")
	if prep, _ := body["prep"].(string); prep != "" {
		text += " " + prep
	}
	_, err := Append(ctx, tx, Event{Stream: CardsStream(repo), Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing " + text, name: body}})
	return err
}

// errCardOptions: a request whose options are not the kind's choices.
var errCardOptions = errors.New("landing card: options are not the kind's choices")

// cardOptions are a kind's options as letters, for a choice set it offers.
func cardOptions(kind string, choices []string) (map[string]any, error) {
	sets := cardChoices[kind]
	if !slices.ContainsFunc(sets, func(set []string) bool { return slices.Equal(set, choices) }) {
		return nil, errCardOptions
	}
	out := map[string]any{}
	for i, c := range choices {
		out[string(rune('a'+i))] = c
	}
	return out, nil
}
