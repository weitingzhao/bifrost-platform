package debtimport

import (
	"slices"
	"strings"
	"unicode"
)

// Read clauses independently: annotations on cost/risk cannot erase the gate.
func approvalClauses(l line) map[string]field {
	out := map[string]field{}
	if !strings.HasPrefix(l.text, "- 审批") {
		return out
	}
	at := len("- 审批")
	for i, clause := range strings.Split(l.text[at:], "·") {
		key, value := "gate", clause
		if i == 0 {
			value = strings.TrimLeft(value, ":： \t")
		} else {
			value = strings.TrimSpace(value)
			switch {
			case strings.HasPrefix(value, "代价"):
				key = "cost"
				value = strings.TrimSpace(strings.TrimPrefix(value, "代价"))
			case strings.HasPrefix(value, "风险"):
				key = "risk"
				value = strings.TrimSpace(strings.TrimPrefix(value, "风险"))
			case strings.HasPrefix(value, "repos:"):
				key = "repos"
				value = strings.TrimSpace(strings.TrimPrefix(value, "repos:"))
			default:
				at += len(clause) + len("·")
				continue
			}
		}
		value = strings.TrimSpace(value)
		pos := strings.Index(clause, value)
		if key == "cost" || key == "risk" {
			tokens := strings.FieldsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '（' || r == '(' })
			if len(tokens) > 0 {
				value = tokens[0]
			}
		}
		out[key] = field{key: key, value: value, start: l.start + at + pos, end: l.start + at + pos + len(value)}
		at += len(clause) + len("·")
	}
	return out
}

func (p *parser) approval(id, body string, start, idx int, d *Debt) ([]string, string, bool) {
	repos := []string{}
	gate := "other"
	wantsStart := false
	found := false
	for _, l := range lines(body, start) {
		clauses := approvalClauses(l)
		if len(clauses) == 0 {
			continue
		}
		found = true
		p.addText(id, "import_raw", "审批", l.text)
		if f, ok := clauses["repos"]; ok {
			for _, repo := range strings.Split(f.value, ",") {
				repo = strings.Trim(strings.TrimSpace(repo), "`")
				if repo == "" {
					continue
				}
				repos = append(repos, repo)
				if !slices.Contains(p.knownRepos(), repo) {
					p.finding(id, 7, "unrecognized_repo", repo+"; preserved literally from repos")
				}
			}
		}
		if f, ok := clauses["cost"]; ok {
			d.Cost = f.value
			if !slices.Contains([]string{"S", "M", "L", "XL"}, d.Cost) {
				p.finding(id, 4, "unmapped_cost", f.value)
				d.Cost = ""
			}
		}
		if f, ok := clauses["risk"]; ok {
			d.Risk = f.value
			if d.Risk == "med" {
				d.Risk = "medium"
			}
			if !slices.Contains([]string{"low", "medium", "high"}, d.Risk) {
				p.finding(id, 4, "unmapped_risk", f.value)
				d.Risk = ""
			}
		}
		a := clauses["gate"].value
		var mapped bool
		gate, wantsStart, mapped = classifyApproval(a)
		if !mapped {
			p.finding(id, 4, "unmapped_approval", a)
		}
		if strings.Contains(a, "另批") || (gate == "none" && containsAny(a, "要 Owner", "要你", "由 Owner", "再问")) {
			p.finding(id, 4, "conditional_approval", a)
		}
	}
	if !found {
		p.finding(id, 4, "missing_approval", "no approval line; apply_gate is other until reviewed")
	}
	return repos, gate, wantsStart
}

func (p *parser) bindApproval(body string, start, idx, firstPart, count int, d Debt) {
	for _, l := range lines(body, start) {
		c := approvalClauses(l)
		for _, key := range []string{"cost", "risk", "gate", "repos"} {
			f, ok := c[key]
			if !ok {
				continue
			}
			paths := []string{}
			render := "string"
			switch key {
			case "cost":
				if d.Cost != "" {
					paths = append(paths, pointer("debt", idx, "cost"))
				}
			case "risk":
				if d.Risk != "" {
					paths = append(paths, pointer("debt", idx, "risk"))
				}
			case "gate", "repos":
				column := "repo"
				render = "repos"
				if key == "gate" {
					column = "apply_gate"
					render = "gates"
				}
				for i := 0; i < count; i++ {
					paths = append(paths, pointer("debt_part", firstPart+i, column))
				}
			}
			if len(paths) > 0 {
				p.bind(0, f.start, f.end, paths, render)
			}
		}
	}
}

func (p *parser) hasDecisionGroup(id string) bool {
	for _, sec := range sections(p.ledger, "## ") {
		if sec.title != "需要你拍板" {
			continue
		}
		for _, l := range lines(p.ledger[sec.body:sec.end], 0) {
			if startsAny(l.text, "- 项：", "- 项:") && slices.Contains(tdMention.FindAllString(l.text, -1), id) {
				p.finding(id, 4, "approval_direction_group", "existing pending group supplies the start reference; no duplicate decision")
				return true
			}
		}
	}
	return false
}
