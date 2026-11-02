package debtimport

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

var (
	tdHeader  = regexp.MustCompile(`^### (TD-[1-9][0-9]*)$`)
	tdMention = regexp.MustCompile(`\bTD-[1-9][0-9]*\b`)
	titleLine = regexp.MustCompile(`^\*\*(P\d) · ([^·]+?) · (.+)\*\*$`)
	fieldLine = regexp.MustCompile(`^- (?:\*\*(.+?)\*\*|(验收))[:：][ \t]?(.*)$`)
	stateCut  = regexp.MustCompile(`（|\(|——|。|｜|：`)
	watchDate = regexp.MustCompile(`到\s*(\d\d)-(\d\d)`)
	shortSHA  = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	fullDate  = regexp.MustCompile(`\b20\d\d-\d\d-\d\d\b`)
)

type line struct {
	text       string
	start, end int
}
type section struct {
	title            string
	start, body, end int
}
type field struct {
	key, value string
	start, end int
}
type parser struct {
	req              Request
	report           Report
	opts             Options
	zone             *time.Location
	ledger, ratchets string
	index            map[string]int
	spans            [2][]span
	entriesAt        []entryRange
}

// Parse consumes UTF-8 Markdown and returns a reviewable request even when
// individual fields need adjudication. Structural damage is an error. Missing
// history and unmappable states remain null/empty, with explicit findings.
func Parse(ledger, ratchets []byte, opts Options) (Request, Report, error) {
	if opts.ImportedAt.IsZero() {
		return Request{}, Report{}, fmt.Errorf("imported_at must be supplied")
	}
	if !utf8.Valid(ledger) || !utf8.Valid(ratchets) {
		return Request{}, Report{}, fmt.Errorf("ledgers must be UTF-8")
	}
	zone, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return Request{}, Report{}, err
	}
	p := &parser{opts: opts, zone: zone, ledger: string(ledger), ratchets: string(ratchets), index: map[string]int{}}
	p.req = Request{Format: Format, Source: Source{opts.ImportedAt.UTC(), digest(string(ledger)), digest(string(ratchets)), slices.Clone(opts.Projects), opts.RepoRoots}, Floor: 488,
		Debts: []Debt{}, Texts: []Text{}, Evidence: []Evidence{}, Parts: []Part{}, Edges: []Edge{}, Refs: []Ref{}, Runs: []AcceptanceRun{}, Decisions: []Decision{}, Ratchets: []Ratchet{}, Gaps: []Gap{}, ProjectNotes: []ProjectNote{}, LastSeen: []LastSeen{}, Umbrellas: []Umbrella{}}
	p.report.Findings = []Finding{}
	p.req.FloorSources = slices.Clone(opts.FloorSources)
	for _, source := range opts.FloorSources {
		if source.DebtID != "" && !tdHeader.MatchString("### "+source.DebtID) {
			return Request{}, Report{}, fmt.Errorf("invalid floor source id")
		}
		p.req.Floor = max(p.req.Floor, number(source.DebtID))
		p.finding("floor", 12, "floor_source", source.Kind+" "+source.Repository+" "+source.Ref+" "+source.DebtID+" "+source.Commit)
		if source.Kind == "reserved_unspecified" {
			p.finding("floor", 12, "incomplete_floor", "reserved IDs were not supplied; pass an explicit list or none")
		}
	}
	if len(opts.FloorSources) == 0 {
		p.finding("floor", 12, "incomplete_floor", "history, main trailers and reserved ids were not supplied; 488 is only the frozen baseline")
	}
	if err = p.entries(); err != nil {
		return Request{}, Report{}, err
	}
	if err = p.sections(); err != nil {
		return Request{}, Report{}, err
	}
	if err = p.readRatchets(); err != nil {
		return Request{}, Report{}, err
	}
	p.lastSeen()
	if err = p.layout(); err != nil {
		return Request{}, Report{}, err
	}
	p.finishReport()
	return p.req, p.report, nil
}

func lines(s string, offset int) []line {
	out := []line{}
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		n := i + 1
		if i < 0 {
			i = len(s)
			n = i
		}
		out = append(out, line{strings.TrimSuffix(s[:i], "\r"), offset, offset + i})
		s = s[n:]
		offset += n
	}
	return out
}
func sections(s string, level string) []section {
	out := []section{}
	for _, l := range lines(s, 0) {
		if strings.HasPrefix(l.text, level) {
			if len(out) > 0 {
				out[len(out)-1].end = l.start
			}
			out = append(out, section{strings.TrimPrefix(l.text, level), l.start, min(l.end+1, len(s)), len(s)})
		}
	}
	return out
}

func (p *parser) entries() error {
	sec := ""
	starts := []line{}
	ends := map[int]int{}
	last := -1
	for _, l := range lines(p.ledger, 0) {
		if strings.HasPrefix(l.text, "## ") {
			if last >= 0 {
				ends[last] = l.start
				last = -1
			}
			sec = strings.TrimPrefix(l.text, "## ")
		}
		if strings.HasPrefix(l.text, "### ") && last >= 0 {
			ends[last] = l.start
			last = -1
		}
		if m := tdHeader.FindStringSubmatch(l.text); m != nil {
			if sec != "条目" {
				return fmt.Errorf("%s outside ## 条目 at byte %d", m[1], l.start)
			}
			starts = append(starts, l)
			last = l.start
			ends[last] = len(p.ledger)
		} else if strings.HasPrefix(l.text, "### TD-") {
			return fmt.Errorf("malformed debt heading %q", l.text)
		}
	}
	if len(starts) == 0 {
		return fmt.Errorf("no entries under ## 条目")
	}
	sort.Slice(starts, func(i, j int) bool {
		return number(strings.TrimPrefix(starts[i].text, "### ")) < number(strings.TrimPrefix(starts[j].text, "### "))
	})
	for _, l := range starts {
		id := strings.TrimPrefix(l.text, "### ")
		if _, exists := p.index[id]; exists {
			return fmt.Errorf("duplicate debt %s", id)
		}
		p.entriesAt = append(p.entriesAt, entryRange{l.start, ends[l.start], id})
		p.bind(0, l.start+4, l.end, []string{pointer("debt", len(p.req.Debts), "debt_id")}, "string")
		if err := p.entry(id, l.end+1, ends[l.start]); err != nil {
			return err
		}
	}
	return nil
}

func fields(s string, offset int) []field {
	ll := lines(s, offset)
	out := []field{}
	active := -1
	for _, l := range ll {
		m := fieldLine.FindStringSubmatchIndex(l.text)
		isApproval := strings.HasPrefix(l.text, "- 审批")
		isTitle := titleLine.MatchString(l.text)
		isResult := startsAny(l.text, "- 验收结果:", "- 验收结果：")
		if m != nil || isApproval || isTitle || isResult || strings.HasPrefix(l.text, "### ") {
			if active >= 0 {
				out[active].end = l.start
				active = -1
			}
		}
		if m == nil || isApproval {
			continue
		}
		key := ""
		if m[2] >= 0 {
			key = l.text[m[2]:m[3]]
		} else {
			key = l.text[m[4]:m[5]]
		}
		out = append(out, field{key: key, start: l.start + m[6], end: offset + len(s)})
		active = len(out) - 1
	}
	for i := range out {
		f := &out[i]
		raw := s[f.start-offset : f.end-offset]
		f.value = strings.TrimRight(raw, "\r\n \t")
		f.end = f.start + len(f.value)
	}
	return out
}

var fieldKinds = map[string]string{
	"Claim": "claim", "主张": "claim", "Measured": "measured", "Evidence": "measured", "证据": "measured",
	"Impact": "impact", "影响": "impact", "Fix": "fix", "修法": "fix", "Ratchet": "ratchet_plan", "防线": "ratchet_plan",
	"现在": "now", "下一步": "next", "验收": "acceptance_text", "验收命令": "acceptance_text", "后续": "follow_up_text",
}

func (p *parser) entry(id string, start, end int) error {
	body := p.ledger[start:end]
	idx := len(p.req.Debts)
	d := Debt{DebtID: id, Flow: "manual", Visibility: "project", Tags: []string{}, AcceptanceJSON: []json.RawMessage{}, StateSince: p.req.Source.ImportedAt}
	// The title regexp deliberately matches the existing renderer, including
	// titles appearing after one or more delivery notes.
	found := false
	for _, l := range lines(body, start) {
		if m := titleLine.FindStringSubmatchIndex(l.text); m != nil {
			if found {
				return fmt.Errorf("%s has multiple title lines", id)
			}
			found = true
			d.Priority = l.text[m[2]:m[3]]
			d.Area = strings.TrimSpace(l.text[m[4]:m[5]])
			d.Title = strings.TrimSpace(l.text[m[6]:m[7]])
			for j, key := range []string{"priority", "area", "title"} {
				a, b := m[2+j*2], m[3+j*2]
				p.bind(0, l.start+a, l.start+b, []string{pointer("debt", idx, key)}, "string")
			}
		}
	}
	if !found || !slices.Contains([]string{"P0", "P1", "P2", "P3"}, d.Priority) {
		return fmt.Errorf("%s has no valid priority/area/title", id)
	}
	if utf8.RuneCountInString(d.Title) > 400 {
		p.finding(id, 2, "title_too_long", "title exceeds 400 characters; retained without truncation")
	}
	ff := fields(body, start)
	var state *field
	for i := range ff {
		if ff[i].key == "状态" {
			if state != nil {
				p.finding(id, 5, "duplicate_state", "last state field wins")
			}
			state = &ff[i]
		}
	}
	if state != nil {
		p.readState(&d, *state, idx)
	} else {
		p.finding(id, 5, "unmapped_state", "state field is missing")
	}
	repos, gate, wantsStart := p.approval(id, body, start, idx, &d)
	for _, f := range ff {
		if f.key == "状态" {
			continue
		}
		kind := fieldKinds[f.key]
		if kind == "" {
			kind = "note"
		}
		if f.key == "验收结果" {
			kind = "import_raw"
			p.acceptance(id, f.value, f.value)
		}
		if f.value == "" {
			continue
		}
		n := p.addText(id, kind, f.key, f.value)
		p.bind(0, f.start, f.end, []string{pointer("debt_text", n-1, "body")}, "string")
		p.bindFieldHeading(f, n)
		if kind == "measured" {
			p.evidence(id, f.value, repos)
		}
	}
	p.plainAcceptanceResults(id, body, start)
	if len(repos) == 0 {
		repos = p.inferredRepos(id)
		if len(repos) > 0 {
			p.finding(id, 7, "inferred_repos", strings.Join(repos, ", ")+"; inferred, pending crew review")
			for _, repo := range repos {
				p.finding(id, 7, "inferred_repo_part", repo+"; inferred, pending crew review")
			}
		} else {
			p.finding(id, 7, "missing_repos", "no repository could be inferred from evidence paths")
		}
	}
	firstPart := len(p.req.Parts)
	for i, repo := range repos {
		p.req.Parts = append(p.req.Parts, Part{len(p.req.Parts) + 1, id, repo, i + 1, gate})
	}
	p.bindApproval(body, start, idx, firstPart, len(repos), d)
	d.Project = p.project(id, repos)
	if d.Project != "" {
		p.finding(id, 7, "project_suggestion", d.Project+"; inferred from owns, pending crew review")
	}
	if wantsStart && !p.hasDecisionGroup(id) {
		n := len(p.req.Decisions) + 1
		p.req.Decisions = append(p.req.Decisions, Decision{n, "Approval direction for " + id, approvalText(body), "pending"})
		p.req.Refs = append(p.req.Refs, Ref{DebtID: id, RefKind: "waits_on_owner", DebtDecisionID: n, Covers: "start"})
		p.finding(id, 4, "approval_direction", "start gate uses a request-local pending decision; no approval is inferred")
	}
	if p.opts.History != nil {
		t, err := p.opts.History.FirstSeen(id)
		if err != nil || t.IsZero() {
			p.finding(id, 12, "filed_at_unavailable", "git history lookup did not return a first-seen time")
		} else {
			t = t.UTC()
			d.FiledAt = &t
		}
	} else {
		p.finding(id, 12, "filed_at_unavailable", "no git history provider; filed_at remains null")
	}
	if !slices.ContainsFunc(p.req.Evidence, func(e Evidence) bool { return e.DebtID == id }) {
		p.finding(id, 6, "missing_evidence", "no Evidence, Measured or 证据 field")
	}
	if !slices.ContainsFunc(p.req.Texts, func(t Text) bool { return t.DebtID == id && t.TextKind == "acceptance_text" }) {
		p.finding(id, 9, "missing_acceptance", "acceptance specification remains empty")
	}
	p.index[id] = idx
	p.req.Debts = append(p.req.Debts, d)
	return nil
}

func (p *parser) readState(d *Debt, f field, idx int) {
	raw := strings.TrimSpace(strings.ReplaceAll(f.value, "**", ""))
	head := strings.TrimSpace(stateCut.Split(raw, 2)[0])
	d.State = map[string]string{"未开始": "open", "在做": "doing", "观察中": "watching", "待你签收": "awaiting_signoff"}[head]
	if d.State == "" {
		p.finding(d.DebtID, 5, "unmapped_state", f.value)
	}
	kind := "import_raw"
	if d.State == "watching" {
		kind = "watch"
	}
	n := p.addText(d.DebtID, kind, "状态", f.value)
	if d.State == "watching" {
		d.WatchTextID = n
		t := p.req.Source.ImportedAt
		if m := watchDate.FindStringSubmatch(raw); m != nil {
			month, _ := strconv.Atoi(m[1])
			day, _ := strconv.Atoi(m[2])
			v := time.Date(2026, time.Month(month), day, 23, 59, 0, 0, p.zone)
			if int(v.Month()) != month || v.Day() != day {
				p.finding(d.DebtID, 5, "invalid_watch_date", m[0])
			} else {
				t = v
			}
		} else {
			p.finding(d.DebtID, 5, "undated_watch", "watch_until uses imported_at")
		}
		d.WatchUntil = &t
		if t.Before(p.req.Source.ImportedAt) {
			p.finding(d.DebtID, 5, "overdue_watch", t.Format(time.RFC3339))
		}
	}
	paths := []string{pointer("debt", idx, "state"), pointer("debt", idx, "watch_until")}
	if d.State == "" {
		paths = []string{pointer("debt_text", n-1, "body")}
		p.bind(0, f.start, f.end, paths, "string")
		return
	}
	p.bind(0, f.start, f.end, paths, "state")
}
