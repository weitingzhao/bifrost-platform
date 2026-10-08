package progress

import (
	"regexp"
	"strings"
)

const (
	catDebt = "debt"
	catPlan = "plan"
	catLane = "lane"
)

var (
	heading = regexp.MustCompile(`(?m)^###[ \t]+(TD-\d+|W-\d+)[ \t]*$`)
	field   = regexp.MustCompile(`(?m)^- \*\*(.+?)\*\*[:：]\s?(.*)$`)
	bold    = regexp.MustCompile(`(?m)^\*\*(.+)\*\*\s*$`)
	pTitle  = regexp.MustCompile(`^P\d+ · .+? · (.+)$`)
	signoff = regexp.MustCompile(`\*\*(TD-\d+)\*\*`)
	workID  = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(TD-\d+|W-\d+|LANE-[A-Z0-9]+)`)
)

func findIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range workID.FindAllStringSubmatch(s, -1) {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func statusKey(status string) string {
	status = strings.TrimSpace(status)
	if i := strings.IndexAny(status, "（("); i >= 0 {
		status = status[:i]
	}
	return strings.TrimSpace(status)
}

func blocks(text string) []struct{ id, body string } {
	idx := heading.FindAllStringSubmatchIndex(text, -1)
	out := make([]struct{ id, body string }, 0, len(idx))
	for i, m := range idx {
		id := text[m[2]:m[3]]
		bodyStart := m[1]
		bodyEnd := len(text)
		if i+1 < len(idx) {
			bodyEnd = idx[i+1][0]
		}
		out = append(out, struct{ id, body string }{id, text[bodyStart:bodyEnd]})
	}
	return out
}

func fields(body string) map[string]string {
	out := map[string]string{}
	for _, m := range field.FindAllStringSubmatch(body, -1) {
		if _, ok := out[m[1]]; ok {
			continue
		}
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func titleOf(body string) string {
	m := bold.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	inner := strings.TrimSpace(m[1])
	if t := pTitle.FindStringSubmatch(inner); t != nil {
		return strings.TrimSpace(t[1])
	}
	return inner
}

func signoffIDs(debt string) map[string]bool {
	out := map[string]bool{}
	const head = "## 待你签收"
	i := strings.Index(debt, head)
	if i < 0 {
		return out
	}
	rest := debt[i+len(head):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	for _, m := range signoff.FindAllStringSubmatch(rest, -1) {
		out[m[1]] = true
	}
	return out
}

func categoryOf(raw string) string {
	switch strings.TrimSpace(raw) {
	case "债", catDebt:
		return catDebt
	case "道", catLane:
		return catLane
	default:
		return catPlan
	}
}

func parseDocs(debt, work string) []Item {
	signed := signoffIDs(debt)
	var items []Item
	for _, b := range blocks(debt) {
		if !strings.HasPrefix(b.id, "TD-") {
			continue
		}
		f := fields(b.body)
		title := titleOf(b.body)
		if title == "" {
			title = b.id
		}
		status := f["状态"]
		if status == "" {
			status = "未开始"
		}
		items = append(items, Item{
			ID: b.id, Title: title, Category: catDebt, Status: status,
			AcceptResult: f["验收结果"], Match: f["匹配"],
			AwaitingSignoff: signed[b.id],
		})
	}
	for _, b := range blocks(work) {
		if !strings.HasPrefix(b.id, "W-") {
			continue
		}
		f := fields(b.body)
		title := titleOf(b.body)
		if title == "" {
			title = b.id
		}
		status := f["状态"]
		if status == "" {
			status = "未开始"
		}
		items = append(items, Item{
			ID: b.id, Title: title, Category: categoryOf(f["类别"]), Status: status,
			AcceptResult: f["验收结果"], Match: f["匹配"],
		})
	}
	return items
}
