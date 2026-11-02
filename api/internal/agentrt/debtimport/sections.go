package debtimport

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

var (
	waveHeader   = regexp.MustCompile(`^第 (\d+) 波`)
	boldBullet   = regexp.MustCompile(`^- \*\*(.+?)\*\*\s*—\s*(.*)$`)
	seenMark     = regexp.MustCompile(`(?m)^上次查看[：:]\s*([a-f0-9]{7,40}).*$`)
	followMarker = regexp.MustCompile(`(?:无)?后续[：:]`)
)

func (p *parser) tag(id, tag string) {
	if i, ok := p.index[id]; ok && !slices.Contains(p.req.Debts[i].Tags, tag) {
		p.req.Debts[i].Tags = append(p.req.Debts[i].Tags, tag)
	}
}
func (p *parser) sections() error {
	for _, sec := range sections(p.ledger, "## ") {
		body := p.ledger[sec.body:sec.end]
		switch {
		case sec.title == "待你签收":
			p.signoffs(body, sec.body)
		case strings.HasPrefix(sec.title, "主题"):
			for _, l := range lines(body, sec.body) {
				if m := boldBullet.FindStringSubmatch(l.text); m != nil {
					for _, id := range tdMention.FindAllString(m[2], -1) {
						p.tag(id, "theme:"+slug(m[1]))
					}
				}
			}
		case sec.title == "先看这几条":
			for _, l := range lines(body, sec.body) {
				if m := boldBullet.FindStringSubmatch(l.text); m != nil && tdMention.MatchString(m[1]) {
					p.tag(m[1], "look-first")
				}
			}
		case sec.title == "还债顺序":
			p.order(body, sec.body)
		case sec.title == "需要你拍板":
			p.decisions(body, sec.body)
		case strings.HasPrefix(sec.title, "数据边界"):
			p.gaps(body, sec.body)
		case strings.HasPrefix(sec.title, "没覆盖到的") || sec.title == "怎么做的":
			p.projectNote(sec.title, body, sec.body, 0)
		}
	}
	for i := range p.req.Debts {
		slices.Sort(p.req.Debts[i].Tags)
	}
	return nil
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return b.String()
}
func (p *parser) projectNote(heading, body string, start, doc int) {
	value := strings.TrimRight(body, "\r\n")
	if value == "" {
		return
	}
	i := len(p.req.ProjectNotes)
	p.req.ProjectNotes = append(p.req.ProjectNotes, ProjectNote{p.opts.CrewProject, heading, value})
	p.bind(doc, start, start+len(value), []string{pointer("project_notes", i, "body")}, "string")
}
func (p *parser) order(body string, offset int) {
	for _, sec := range sections(body, "### ") {
		part := body[sec.body:sec.end]
		if m := waveHeader.FindStringSubmatch(sec.title); m != nil {
			members := []string{}
			for _, l := range lines(part, 0) {
				if strings.HasPrefix(l.text, "项：") {
					active, _, _ := strings.Cut(l.text, "已还：")
					for _, id := range tdMention.FindAllString(active, -1) {
						p.tag(id, "wave:"+m[1])
						if _, ok := p.index[id]; ok {
							members = append(members, id)
						}
					}
				}
			}
			p.finding("wave:"+m[1], 8, "wave_membership", sec.title+"; active members: "+strings.Join(sortedIDs(members), ", ")+"; absent/closed members receive no tag")
		} else if strings.HasPrefix(sec.title, "数据完整度执行清单") {
			// No real number may be issued by a pure parser. The store allocates
			// and rewrites this request-local handle under its counter lock.
			handle := "import:umbrella:" + strconv.Itoa(len(p.req.Umbrellas)+1)
			i := len(p.req.Umbrellas)
			p.req.Umbrellas = append(p.req.Umbrellas, Umbrella{handle, sec.title, strings.TrimRight(part, "\r\n")})
			p.bind(0, offset+sec.body, offset+sec.body+len(p.req.Umbrellas[i].Body), []string{pointer("umbrellas", i, "body")}, "string")
			members := []string{}
			for _, l := range lines(part, 0) {
				if strings.HasPrefix(l.text, "|") {
					cells := tableCells(l.text)
					if len(cells) > 0 {
						members = append(members, tdMention.FindAllString(cells[0].value, -1)...)
					}
				}
			}
			for _, id := range sortedIDs(members) {
				if _, ok := p.index[id]; ok {
					p.req.Edges = append(p.req.Edges, Edge{"part_of", id, handle})
				} else {
					p.finding(id, 8, "closed_umbrella_member", "member is absent from entries")
				}
			}
			p.finding(handle, 8, "umbrella_needs_id", "allocate a new debt id and choose project/priority before importing part_of edges")
		}
	}
}

func (p *parser) signoffs(body string, offset int) {
	for _, l := range lines(body, offset) {
		m := boldBullet.FindStringSubmatchIndex(l.text)
		if m == nil {
			continue
		}
		id := l.text[m[2]:m[3]]
		if !tdMention.MatchString(id) {
			continue
		}
		i, ok := p.index[id]
		if !ok {
			p.finding(id, 9, "closed_signoff_member", l.text)
			continue
		}
		if p.req.Debts[i].State != "awaiting_signoff" {
			p.finding(id, 9, "signoff_state_mismatch", p.req.Debts[i].State)
		}
		content := l.text[m[4]:m[5]]
		accept := strings.Index(content, "验收：")
		if accept < 0 {
			accept = strings.Index(content, "验收:")
		}
		guard := strings.Index(content, "防线：")
		if guard < 0 {
			guard = strings.Index(content, "防线:")
		}
		follow := followMarker.FindStringIndex(content)
		if accept < 0 || guard < accept || follow == nil || follow[0] < guard {
			p.finding(id, 9, "unmapped_signoff", l.text)
			p.addText(id, "import_raw", "待你签收", l.text)
			continue
		}
		// Chinese/full-width and ASCII colons have different byte lengths.
		aStart := accept + len("验收") + len(":")
		if strings.HasPrefix(content[accept+len("验收"):], "：") {
			aStart += 2
		}
		gStart := guard + len("防线") + 1
		if strings.HasPrefix(content[guard+len("防线"):], "：") {
			gStart += 2
		}
		segments := []struct {
			kind string
			a, b int
		}{
			{"signoff_summary", 0, accept}, {"import_raw", aStart, guard}, {"ratchet_text", gStart, follow[0]}, {"follow_up_text", follow[0], len(content)},
		}
		for _, s := range segments {
			value := content[s.a:s.b]
			n := p.addText(id, s.kind, "待你签收", value)
			if n > 0 {
				p.bind(0, l.start+m[4]+s.a, l.start+m[4]+s.b, []string{pointer("debt_text", n-1, "body")}, "string")
			}
		}
		p.acceptance(id, content[aStart:guard], content)
	}
}

func (p *parser) decisions(body string, offset int) {
	for _, sec := range sections(body, "### ") {
		raw := strings.TrimRight(body[sec.body:sec.end], "\r\n")
		i := len(p.req.Decisions)
		n := i + 1
		p.req.Decisions = append(p.req.Decisions, Decision{n, sec.title, raw, "pending"})
		p.bind(0, offset+sec.start+len("### "), offset+sec.start+len("### ")+len(sec.title), []string{pointer("debt_decision", i, "title")}, "string")
		p.bind(0, offset+sec.body, offset+sec.body+len(raw), []string{pointer("debt_decision", i, "body_text")}, "string")
		members := []string{}
		for _, l := range lines(raw, 0) {
			if strings.HasPrefix(l.text, "- 项：") || strings.HasPrefix(l.text, "- 项:") {
				members = append(members, tdMention.FindAllString(l.text, -1)...)
			}
		}
		alive := 0
		advanced := []string{}
		for _, id := range sortedIDs(members) {
			if j, ok := p.index[id]; ok {
				alive++
				p.req.Refs = append(p.req.Refs, Ref{DebtID: id, RefKind: "waits_on_owner", DebtDecisionID: n, Covers: "start"})
				if s := p.req.Debts[j].State; s == "watching" || s == "awaiting_signoff" {
					advanced = append(advanced, id)
				}
			} else {
				p.finding(id, 10, "closed_decision_member", sec.title)
			}
		}
		if alive == 0 || len(advanced) > 0 {
			// Rule 10 keeps the start reference of every member still in the
			// ledger; crew marks the decision dropped or asks again.
			p.finding(fmt.Sprintf("decision:%03d", n), 10, "stale_decision", sec.title+"; absent or already landed members: "+strings.Join(advanced, ", ")+"; members still listed keep waits_on_owner(start) until crew marks this decision dropped or asks again")
		}
		p.finding(fmt.Sprintf("decision:%03d", n), 10, "decision_review_required", "pending only; crew must verify before asking: "+sec.title)
	}
}
func (p *parser) gaps(body string, offset int) {
	intro, _, _ := strings.Cut(body, "\n- ")
	reopen := ""
	if strings.Contains(intro, "订阅或市场变化时重开") {
		reopen = "订阅或市场变化时重开"
	}
	for _, l := range lines(body, offset) {
		if m := boldBullet.FindStringSubmatchIndex(l.text); m != nil {
			i := len(p.req.Gaps)
			title := l.text[m[2]:m[3]]
			reason := l.text[m[4]:m[5]]
			project := ""
			candidates := []string{}
			for _, proj := range p.opts.Projects {
				for _, own := range proj.Owns {
					for _, m := range pathLine.FindAllStringSubmatch(reason, -1) {
						if ownsMatch(own, m[1]) && !slices.Contains(candidates, proj.Key) {
							candidates = append(candidates, proj.Key)
						}
					}
				}
			}
			if len(candidates) == 1 {
				project = candidates[0]
			}
			id := "AG-" + strconv.Itoa(i+1)
			p.req.Gaps = append(p.req.Gaps, Gap{id, project, "data_boundary", title, reason, reopen, "accepted"})
			if project == "" {
				p.finding(id, 11, "gap_project_required", "source has no unambiguous owns path")
			}
			if reopen == "" {
				p.finding(id, 11, "gap_reopen_required", "source does not state when to reopen")
			}
			p.bind(0, l.start+m[2], l.start+m[3], []string{pointer("accepted_gap", i, "title")}, "string")
			p.bind(0, l.start+m[4], l.start+m[5], []string{pointer("accepted_gap", i, "reason")}, "string")
		}
	}
}

func (p *parser) lastSeen() {
	m := seenMark.FindStringSubmatch(p.ledger)
	if m == nil {
		p.finding("ledger", 12, "missing_last_seen", "no 上次查看 marker")
		return
	}
	for _, proj := range p.opts.Projects {
		p.req.LastSeen = append(p.req.LastSeen, LastSeen{proj.Key, m[1], m[0]})
	}
	if len(p.req.LastSeen) > 0 {
		loc := seenMark.FindStringSubmatchIndex(p.ledger)
		paths := []string{}
		for i := range p.req.LastSeen {
			paths = append(paths, pointer("last_seen", i, "commit"))
		}
		p.bind(0, loc[2], loc[3], paths, "same")
	}
	if len(p.opts.Projects) == 0 {
		p.finding("ledger", 12, "missing_projects", "last-seen marker cannot be assigned without the projects catalog")
	}
}

type cell struct {
	value      string
	start, end int
}

func tableCells(s string) []cell {
	// Pipes inside code spans and escaped pipes are content, not delimiters.
	boundaries := []int{}
	ticks := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '`' {
			j := i
			for j < len(s) && s[j] == '`' {
				j++
			}
			n := j - i
			if ticks == 0 {
				ticks = n
			} else if ticks == n {
				ticks = 0
			}
			i = j - 1
			continue
		}
		if s[i] == '|' && ticks == 0 {
			boundaries = append(boundaries, i)
		}
	}
	out := []cell{}
	for i := 0; i+1 < len(boundaries); i++ {
		a, b := boundaries[i]+1, boundaries[i+1]
		raw := s[a:b]
		value := strings.TrimSpace(raw)
		a += len(raw) - len(strings.TrimLeft(raw, " \t"))
		out = append(out, cell{value, a, a + len(value)})
	}
	return out
}

func (p *parser) readRatchets() error {
	for _, sec := range sections(p.ratchets, "## ") {
		body := p.ratchets[sec.body:sec.end]
		if strings.HasPrefix(sec.title, "各类债现在挡没挡住") || strings.HasPrefix(sec.title, "待建防线") {
			p.projectNote(sec.title, body, sec.body, 1)
			continue
		}
		if sec.title != "现有防线" {
			continue
		}
		for _, l := range lines(body, sec.body) {
			if !strings.HasPrefix(l.text, "|") {
				continue
			}
			cells := tableCells(l.text)
			if len(cells) != 5 && strings.Count(l.text, "|") == 6 {
				// A legacy row has unbalanced backticks, but exactly five
				// pipe-separated cells. Preserve their bytes and report it.
				cells = nil
				at := 1
				for _, raw := range strings.Split(l.text, "|")[1:6] {
					v := strings.TrimSpace(raw)
					a := at + len(raw) - len(strings.TrimLeft(raw, " \t"))
					cells = append(cells, cell{v, a, a + len(v)})
					at += len(raw) + 1
				}
				p.finding("R-"+strconv.Itoa(len(p.req.Ratchets)+1), 13, "malformed_table_markdown", "unbalanced code delimiters; five pipe-separated cells preserved")
			}
			if len(cells) > 0 && (cells[0].value == "防线" || strings.Trim(cells[0].value, "-: ") == "") {
				continue
			}
			if len(cells) > 5 || len(cells) == 0 {
				return fmt.Errorf("ratchet row at byte %d has %d columns, want 5", l.start, len(cells))
			}
			i := len(p.req.Ratchets)
			id := "R-" + strconv.Itoa(i+1)
			if len(cells) < 5 {
				p.finding(id, 13, "incomplete_ratchet_row", fmt.Sprintf("source has %d columns; missing trailing columns remain empty", len(cells)))
				for len(cells) < 5 {
					cells = append(cells, cell{})
				}
			}
			strength := strings.FieldsFunc(cells[3].value, func(r rune) bool { return !unicode.IsLetter(r) })
			class := "other"
			if len(strength) > 0 && slices.Contains([]string{"blocking", "warning", "alert", "manual"}, strength[0]) {
				class = strength[0]
			}
			refs := sortedIDs(tdMention.FindAllString(l.text, -1))
			p.req.Ratchets = append(p.req.Ratchets, Ratchet{id, cells[0].value, cells[1].value, cells[2].value, cells[3].value, cells[4].value, class, refs, "active"})
			for j, key := range []string{"name", "location", "guards", "strength", "scope_gaps"} {
				p.bind(1, l.start+cells[j].start, l.start+cells[j].end, []string{pointer("ratchet", i, key)}, "cell")
			}
			for _, td := range refs {
				if _, ok := p.index[td]; ok {
					p.req.Refs = append(p.req.Refs, Ref{DebtID: td, RefKind: "guarded_by", RatchetID: id})
				}
			}
		}
	}
	return nil
}
