package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Presence (W-31 round 3, 3.5; CUT-online-status-2026-10-23 option A: the
// Owner's 「主机掉线或执行者不可用，Console 和参谋长简报 1 分钟内说出来」). A
// host is online while its last poll is at most HostLostAfter old; what it
// offers, and whether its free disk is below the floor, is what its latest
// claim said (RecordHostOffered: a note when that changed, no table or
// column). From those, the controls, the accounts' pools and the jobs'
// candidates, an executor is available when some candidate of it may take
// work now: a host that is online, active, under no stop, pause or drain and
// with room on disk offers a tool the candidate fits, and its account neither
// cools down nor reads at its tenant's quota stop. GET /hosts and GET
// /progress read the hosts' document (HostsSQL), and Progress and the
// controller's presence pass (presencepass.go) read the same view
// (readPresence), so the Console and the Owner's conversation say the same
// thing.
//
// Each kind of presence record of one host or executor has a stream of its
// own (review 526 B2), so its latest state is the stream's last event, read
// through the stream's head (headSQL): two index probes, whatever else the
// runtime has recorded and however long ago the state was set. Nothing else
// is written to these streams.

// HostOffersStream holds what a host's claims offer (RecordHostOffered).
func HostOffersStream(host string) string { return "offers:host:" + host }

// HostPresenceStream holds a host's presence turns (presencepass.go).
func HostPresenceStream(host string) string { return "presence:host:" + host }

// ExecutorPresenceStream holds an executor's presence turns, executor
// named as its actor (exec:claude).
func ExecutorPresenceStream(executor string) string { return "presence:" + executor }

// headSQL is the SQL of the last event of the stream the SQL expression
// stream names, as one jsonb value {"type", "at", "body"}, NULL while the
// stream has none. It is a scalar subquery, so it is read by the stream's
// head and the event's key for each row it is asked for, never joined
// against the events at large (review 526 B2: a LATERAL join of the same
// was planned as a hash join over every event).
func headSQL(stream string) string {
	return `(SELECT jsonb_build_object('type', e.event_type, 'at', e.at, 'body', e.body_json) FROM agentrt.event e
	    WHERE e.event_stream_id = ` + stream + ` AND e.seq = (SELECT s.last_seq FROM agentrt.event_stream s WHERE s.event_stream_id = ` + stream + `))`
}

// HostsSQL is the hosts document at the moment the SQL expression now names,
// with $1 HostLostAfter in seconds: GET /hosts and the projection both read
// it, so a host is online in one exactly when it is in the other (a host is
// lost past HostLostAfter, as TaskStatus has it). Each host also carries what
// its latest claim offered (offered, null until one is recorded, with
// offered_at) and whether its free disk was then below the floor (disk_low),
// and whether the controller reports it offline (reported_offline: not
// online, active, and no stop or drain on it or on every host).
func HostsSQL(now string) string {
	return `SELECT COALESCE(jsonb_agg(to_jsonb(h) || jsonb_build_object(
	    'poll_age_s', floor(extract(epoch FROM ` + now + ` - h.last_poll_at)),
	    'online', COALESCE(` + now + ` - h.last_poll_at <= make_interval(secs => $1), false),
	    'live_attempts', (SELECT count(*) FROM agentrt.attempt t WHERE t.host_id = h.host_id AND t.state IN ('leased','running','stale')),
	    'controls', COALESCE((SELECT jsonb_agg(jsonb_build_object('control_id', c.control_id, 'kind', c.control_kind, 'reason', c.reason)
	      ORDER BY c.set_at) FROM agentrt.control c WHERE c.cleared_at IS NULL AND (c.scope = 'global' OR (c.scope = 'host' AND c.scope_ref = h.host_id))), '[]'::jsonb),
	    'offered', o.head->'body'->'offered', 'offered_at', o.head->'at', 'disk_low', COALESCE((o.head->'body'->>'disk_below')::boolean, false),
	    'reported_offline', h.state = 'active' AND NOT COALESCE(` + now + ` - h.last_poll_at <= make_interval(secs => $1), false)
	      AND NOT EXISTS (SELECT 1 FROM agentrt.control c WHERE c.cleared_at IS NULL AND c.control_kind IN ('stop','drain')
	        AND (c.scope = 'global' OR (c.scope = 'host' AND c.scope_ref = h.host_id))))
	  ORDER BY h.host_id), '[]'::jsonb)
	  FROM agentrt.host h
	  CROSS JOIN LATERAL (SELECT ` + headSQL(`'offers:host:' || h.host_id`) + ` AS head OFFSET 0) o`
}

// Offer is a tool a host offers, as its claim named it and its stream records
// it.
type Offer struct {
	Executor string `json:"executor"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (o Offer) tool() Tool { return Tool{Executor: o.Executor, Provider: o.Provider, Model: o.Model} }

func (o Offer) words() string {
	if o.Model == "" {
		return fmt.Sprintf("%s (%s)", o.Executor, o.Provider)
	}
	return fmt.Sprintf("%s (%s, %s)", o.Executor, o.Provider, o.Model)
}

// offersOf is tools as offers, in one order whatever the host's preference:
// a change of preference is not a change of what it offers.
func offersOf(tools []Tool) []Offer {
	out := make([]Offer, 0, len(tools))
	for _, t := range tools {
		o := Offer{Executor: t.Executor, Provider: t.Provider, Model: t.Model}
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b Offer) int {
		return strings.Compare(a.Executor+"\x00"+a.Provider+"\x00"+a.Model, b.Executor+"\x00"+b.Provider+"\x00"+b.Model)
	})
	return out
}

// claimSaid is what a host's latest claim said as its offers stream records
// it; found false while nothing is recorded.
type claimSaid struct {
	found     bool
	offers    []Offer
	diskBelow bool
}

var latestClaimSQL = `SELECT ` + headSQL("$1::text")

// latestClaim reads the head of host's offers stream.
func latestClaim(ctx context.Context, tx pgx.Tx, host string) (claimSaid, error) {
	var c claimSaid
	var head []byte
	if err := tx.QueryRow(ctx, latestClaimSQL, HostOffersStream(host)).Scan(&head); err != nil || head == nil {
		return c, err
	}
	var h struct {
		Body struct {
			Offered   []Offer `json:"offered"`
			DiskBelow bool    `json:"disk_below"`
		} `json:"body"`
	}
	if err := json.Unmarshal(head, &h); err != nil {
		return c, err
	}
	return claimSaid{found: true, offers: h.Body.Offered, diskBelow: h.Body.DiskBelow}, nil
}

// RecordHostOffered records what host's claim offers, and whether the free
// disk it reports is below the floor (false when it reports none), when that
// differs from what its offers stream last recorded (or nothing is recorded
// yet): a note with both. It reports whether it recorded one. A claim
// request calls it once, before its polls; the stream is locked before its
// head is read again, so two claims of one host record a change once.
func RecordHostOffered(ctx context.Context, db DB, host string, tools []Tool, diskBelow bool) (bool, error) {
	offers := offersOf(tools)
	same := func(tx pgx.Tx) (bool, error) {
		last, err := latestClaim(ctx, tx, host)
		return last.found && slices.Equal(last.offers, offers) && last.diskBelow == diskBelow, err
	}
	recorded := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		recorded = false
		if s, err := same(tx); err != nil || s {
			return err
		}
		stream := HostOffersStream(host)
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		if s, err := same(tx); err != nil || s {
			return err
		}
		list, words := make([]any, 0, len(offers)), make([]string, 0, len(offers))
		for _, o := range offers {
			list = append(list, map[string]any{"executor": o.Executor, "provider": o.Provider, "model": o.Model})
			words = append(words, o.words())
		}
		text := "Host " + host + " offers no executor"
		if len(words) > 0 {
			text = "Host " + host + " offers " + strings.Join(words, ", ")
		}
		if diskBelow {
			text += "; its free disk is below the floor"
		}
		_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text + ".", "offered": list, "disk_below": diskBelow}})
		recorded = err == nil
		return err
	})
	return recorded && err == nil, err
}

// HostPresence is a host as the hosts document has it.
type HostPresence struct {
	HostID          string     `json:"host_id"`
	State           string     `json:"state"`
	LastPollAt      *time.Time `json:"last_poll_at"`
	Online          bool       `json:"online"`
	ReportedOffline bool       `json:"reported_offline"`
	DiskLow         bool       `json:"disk_low"`
	LiveAttempts    int        `json:"live_attempts"`
	Offered         *[]Offer   `json:"offered"`
	Controls        []struct {
		Kind string `json:"kind"`
	} `json:"controls"`
}

// held names the control that keeps work from the host, "" for none.
func (h HostPresence) held() string {
	for _, k := range []string{"stop", "pause", "drain"} {
		for _, c := range h.Controls {
			if c.Kind == k {
				return k
			}
		}
	}
	return ""
}

func (h HostPresence) usable() bool {
	return h.Online && h.State == "active" && h.held() == "" && !h.DiskLow
}

// ExecutorPresence is an executor some job's candidate names: whether it is
// available (nil: not known, while a host that could take work has not said
// what it offers), why, the hosts that offer it now, and the jobs it is a
// candidate of.
type ExecutorPresence struct {
	Executor  string   `json:"executor"`
	Available *bool    `json:"available"`
	Why       string   `json:"why"`
	Hosts     []string `json:"hosts"`
	Jobs      []string `json:"jobs"`
}

// presence is the view at one moment: the hosts' document as read, its hosts,
// the executors, and the jobs no executor can take now (each has candidates
// and none of them is available).
type presence struct {
	now       time.Time
	doc       []byte
	hosts     []HostPresence
	executors []ExecutorPresence
	unserved  []string
}

func (p presence) executor(id string) *ExecutorPresence {
	for i := range p.executors {
		if p.executors[i].Executor == id {
			return &p.executors[i]
		}
	}
	return nil
}

// readPresence reads the view at now (zero: the database's clock).
func readPresence(ctx context.Context, tx pgx.Tx, now time.Time) (presence, error) {
	p := presence{now: now}
	if p.now.IsZero() {
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.now); err != nil {
			return p, err
		}
	}
	if err := tx.QueryRow(ctx, HostsSQL("$2::timestamptz"), HostLostAfter.Seconds(), p.now).Scan(&p.doc); err != nil {
		return p, err
	}
	if err := json.Unmarshal(p.doc, &p.hosts); err != nil {
		return p, err
	}
	cp, err := readCapabilities(ctx, tx)
	if err != nil {
		return p, err
	}
	rows, err := tx.Query(ctx, `SELECT job_id, tenant FROM agentrt.job`)
	if err != nil {
		return p, err
	}
	tenants := map[string]string{}
	var job, tenant string
	if _, err = pgx.ForEachRow(rows, []any{&job, &tenant}, func() error { tenants[job] = tenant; return nil }); err != nil {
		return p, err
	}
	cooling := map[string]time.Time{}
	rows, err = tx.Query(ctx, `SELECT vendor_pool_id, cooldown_until FROM agentrt.vendor_pool WHERE cooldown_until > $1`, p.now)
	if err != nil {
		return p, err
	}
	var pool string
	var until time.Time
	if _, err = pgx.ForEachRow(rows, []any{&pool, &until}, func() error {
		if tool, account, _, ok := ParsePool(pool); ok && account == DefaultAccount && until.After(cooling[tool]) {
			cooling[tool] = until
		}
		return nil
	}); err != nil {
		return p, err
	}
	quota, err := quotaByTool(ctx, tx)
	if err != nil {
		return p, err
	}
	stops := map[string]float64{}
	for _, t := range tenants {
		if _, seen := stops[t]; !seen {
			if stops[t], err = quotaStop(ctx, tx, t); err != nil {
				return p, err
			}
		}
	}
	p.executors, p.unserved = judgeExecutors(p.hosts, cp.candidates, tenants, cooling, quota, stops)
	return p, nil
}

// judged is a candidate judged: available or not (nil: not known), why, and
// the usable hosts that offer a tool it fits.
type judged struct {
	ok    *bool
	why   []string
	hosts []string
}

func yes() *bool { t := true; return &t }
func no() *bool  { f := false; return &f }

// rank orders judgements: not available 0, not known 1, available 2.
func rank(ok *bool) int {
	switch {
	case ok == nil:
		return 1
	case *ok:
		return 2
	}
	return 0
}

// judgeExecutors judges each candidate of each job, then each executor (the
// best of its candidates: available over not known over not) and each job
// (unserved when every candidate of it is known not to be available).
func judgeExecutors(hosts []HostPresence, cands map[string][]jobCandidate, tenants map[string]string, cooling map[string]time.Time,
	quota map[string]toolQuota, stops map[string]float64) ([]ExecutorPresence, []string) {
	type acc struct {
		e      ExecutorPresence
		judged []judged
	}
	byExec := map[string]*acc{}
	jobs := make([]string, 0, len(cands))
	for j := range cands {
		jobs = append(jobs, j)
	}
	slices.Sort(jobs)
	unserved := []string{}
	for _, j := range jobs {
		none := len(cands[j]) > 0
		for _, jc := range cands[j] {
			a := byExec[jc.executor]
			if a == nil {
				a = &acc{e: ExecutorPresence{Executor: jc.executor, Hosts: []string{}, Jobs: []string{}}}
				byExec[jc.executor] = a
			}
			if !slices.Contains(a.e.Jobs, j) {
				a.e.Jobs = append(a.e.Jobs, j)
			}
			jd := judgeCandidate(jc, hosts, tenants[j], cooling, quota, stops)
			a.judged = append(a.judged, jd)
			if jd.ok == nil || *jd.ok {
				none = false
			}
		}
		if none {
			unserved = append(unserved, j)
		}
	}
	out := make([]ExecutorPresence, 0, len(byExec))
	for _, a := range byExec {
		e := a.e
		var why []string
		add := func(ws []string) {
			for _, w := range ws {
				if !slices.Contains(why, w) {
					why = append(why, w)
				}
			}
		}
		// The best judgement of its candidates: available (2) over not
		// known (1) over not (0); why is that of the candidates judged so.
		best := 0
		for _, jd := range a.judged {
			for _, h := range jd.hosts {
				if !slices.Contains(e.Hosts, h) {
					e.Hosts = append(e.Hosts, h)
				}
			}
			best = max(best, rank(jd.ok))
		}
		for _, jd := range a.judged {
			if rank(jd.ok) == best {
				add(jd.why)
			}
		}
		e.Available = [](*bool){no(), nil, yes()}[best]
		slices.Sort(e.Hosts)
		e.Why = strings.Join(why, "; ")
		if e.Available != nil && *e.Available {
			e.Why = "offered by " + strings.Join(e.Hosts, ", ")
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b ExecutorPresence) int { return strings.Compare(a.Executor, b.Executor) })
	return out, unserved
}

// judgeCandidate judges one candidate of a job of tenant: its account's
// cooldown and quota stop, as a claim reads them (claim.go, pick.go), then
// the usable hosts' offers.
func judgeCandidate(jc jobCandidate, hosts []HostPresence, tenant string, cooling map[string]time.Time, quota map[string]toolQuota,
	stops map[string]float64) judged {
	tool := toolOf(jc.executor)
	var why []string
	stop, ruled := stops[tenant]
	if !ruled {
		stop = 1
	}
	// A cooldown is the account's spent window: its reading at the stop is
	// the same news.
	switch q := quota[tool]; {
	case !cooling[tool].IsZero():
		why = append(why, fmt.Sprintf("the %s account's pools cool down until %s", tool, cooling[tool].UTC().Format(time.RFC3339)))
	case q.read && q.used >= stop && stop >= 1:
		why = append(why, fmt.Sprintf("its pool is spent (it reads %.0f%% used)", q.used*100))
	case q.read && q.used >= stop:
		why = append(why, fmt.Sprintf("its pool reads %.0f%% used, at or past the %s tenant's quota stop of %.0f%%", q.used*100, tenant, stop*100))
	}
	var by, unknown []string
	for _, h := range hosts {
		if !h.usable() {
			continue
		}
		if h.Offered == nil {
			unknown = append(unknown, h.HostID)
			continue
		}
		for _, o := range *h.Offered {
			if jc.fits(o.tool()) {
				by = append(by, h.HostID)
				break
			}
		}
	}
	switch {
	case len(why) > 0:
		return judged{ok: no(), why: why, hosts: by}
	case len(by) > 0:
		return judged{ok: yes(), hosts: by}
	case len(unknown) > 0:
		return judged{why: []string{"what " + strings.Join(unknown, ", ") + " offer" + plural(len(unknown), "s", "") +
			" is not recorded yet (it is recorded at a host's next claim)"}}
	}
	return judged{ok: no(), why: notOffered(jc, hosts)}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// notOffered says why no host that could take work offers a tool jc fits:
// for each host whose latest claim offered its executor, what keeps it from
// work or that its model does not fit; or that no host offers it.
func notOffered(jc jobCandidate, hosts []HostPresence) []string {
	var why []string
	for _, h := range hosts {
		if h.Offered == nil {
			continue
		}
		var models []string
		fits := false
		for _, o := range *h.Offered {
			if o.Executor == jc.executor {
				models = append(models, o.Provider+" "+cmpOr(o.Model, "(no model named)"))
				fits = fits || jc.fits(o.tool())
			}
		}
		if len(models) == 0 {
			continue
		}
		who := "host " + h.HostID + " offers it but "
		switch {
		case !fits:
			why = append(why, fmt.Sprintf("host %s offers it as %s, which its candidate (%s, model %s) does not fit", h.HostID,
				strings.Join(models, " and "), jc.provider, jc.pattern))
		case h.LastPollAt == nil:
			why = append(why, who+"has never polled")
		case !h.Online:
			why = append(why, who+"has not polled since "+h.LastPollAt.UTC().Format(time.RFC3339))
		case h.State != "active":
			why = append(why, who+"is "+h.State)
		case h.held() != "":
			why = append(why, who+"a "+h.held()+" is in force on it")
		case h.DiskLow:
			why = append(why, who+"its free disk is below the floor")
		}
	}
	if len(why) == 0 {
		return []string{"no host offers it (a host offers what its AGENTD_TOOLS say)"}
	}
	return why
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}
