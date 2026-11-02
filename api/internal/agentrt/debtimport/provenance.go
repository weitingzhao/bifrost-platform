package debtimport

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

type entryRange struct {
	start, end int
	id         string
}

// layoutLabels is the closed set of template labels the ledger repeats
// between bound values (field keys, approval clause names, sign-off labels).
// Like Markdown delimiters they are layout, not text: any other word between
// bindings is prose and is stored explicitly as import_raw.
var layoutLabels = []string{"状态", "审批", "代价", "风险", "repos", "验收结果", "验收", "防线"}

func syntaxRune(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("#*-_|`:：·—", r) }

// layoutWords splits s into maximal runs of non-syntax runes ("words").
func layoutWords(s string) [][2]int {
	out := [][2]int{}
	start := -1
	for i, r := range s {
		if syntaxRune(r) {
			if start >= 0 {
				out = append(out, [2]int{start, i})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(s)})
	}
	return out
}

// Only whitespace, Markdown delimiters and whole closed-set labels can live
// outside records. A label glued to other characters is prose.
func layoutOnly(s string) bool {
	for _, w := range layoutWords(s) {
		if !slices.Contains(layoutLabels, s[w[0]:w[1]]) {
			return false
		}
	}
	return true
}

// proseCore returns [a, b) such that s[:a] and s[b:] are layout and s[a:b]
// starts and ends with a word that is not a label. Labels inside the core stay
// with the prose they belong to.
func proseCore(s string) (int, int) {
	words := layoutWords(s)
	first, last := -1, -1
	for i, w := range words {
		if !slices.Contains(layoutLabels, s[w[0]:w[1]]) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return len(s), len(s)
	}
	return words[first][0], words[last][1]
}
func (p *parser) sourceItem(doc, start int) string {
	if doc == 1 {
		return "ratchets"
	}
	for _, entry := range p.entriesAt {
		if start >= entry.start && start < entry.end {
			return entry.id
		}
	}
	return "ledger"
}
func documentName(doc int) string {
	if doc == 0 {
		return "ledger"
	}
	return "ratchets"
}

func (p *parser) bindFieldHeading(f field, n int) {
	start := strings.LastIndex(p.ledger[:f.start], "\n") + 1
	prefix := p.ledger[start:f.start]
	at := strings.Index(prefix, f.key)
	if at >= 0 {
		p.bind(0, start+at, start+at+len(f.key), []string{pointer("debt_text", n-1, "heading")}, "string")
	}
}

// rawFragments splits unbound source into leading layout, the prose core and
// trailing layout. Only the core is stored, under the item where it starts.
func (p *parser) rawFragments(doc, start, end int) []Fragment {
	raw := []string{p.ledger, p.ratchets}[doc][start:end]
	a, b := proseCore(raw)
	out := []Fragment{}
	if a > 0 {
		out = append(out, Fragment{Syntax: raw[:a], Start: start, End: start + a})
	}
	if b > a {
		core := raw[a:b]
		f := Fragment{Start: start + a, End: start + b, Render: "string"}
		if item := p.sourceItem(doc, start+a); strings.HasPrefix(item, "TD-") {
			n := p.addText(item, "import_raw", "Unmapped source", core)
			f.Paths = []string{pointer("debt_text", n-1, "body")}
		} else {
			n := len(p.req.SourceTexts)
			p.req.SourceTexts = append(p.req.SourceTexts, SourceText{"import_raw", core})
			f.Paths = []string{pointer("source_text", n, "body")}
		}
		out = append(out, f)
	}
	if b < len(raw) {
		out = append(out, Fragment{Syntax: raw[b:], Start: start + b, End: end})
	}
	return out
}

func (p *parser) layout() error {
	for doc, raw := range []string{p.ledger, p.ratchets} {
		sort.Slice(p.spans[doc], func(i, j int) bool { return p.spans[doc][i].start < p.spans[doc][j].start })
		fragments := []Fragment{}
		cursor := 0
		for _, s := range p.spans[doc] {
			if s.start < cursor || s.end > len(raw) {
				return fmt.Errorf("overlapping or out-of-range Markdown binding at %d", s.start)
			}
			if s.start > cursor {
				fragments = append(fragments, p.rawFragments(doc, cursor, s.start)...)
			}
			fragments = append(fragments, Fragment{Paths: s.paths, Render: s.render, Start: s.start, End: s.end})
			cursor = s.end
		}
		if cursor < len(raw) {
			fragments = append(fragments, p.rawFragments(doc, cursor, len(raw))...)
		}
		if doc == 0 {
			p.req.Markdown.Ledger = fragments
		} else {
			p.req.Markdown.Ratchets = fragments
		}
	}
	root, err := projection(p.req)
	if err != nil {
		return err
	}
	for doc, fragments := range [][]Fragment{p.req.Markdown.Ledger, p.req.Markdown.Ratchets} {
		raw := []string{p.ledger, p.ratchets}[doc]
		for _, f := range fragments {
			if len(f.Paths) == 0 {
				continue
			}
			values, err := valuesAt(root, f.Paths)
			if err != nil {
				return err
			}
			rendered, err := renderValue(f.Render, values)
			if err != nil {
				return err
			}
			original := raw[f.Start:f.End]
			if rendered != original {
				p.report.Findings = append(p.report.Findings, Finding{
					Item: p.sourceItem(doc, f.Start), Rule: 3, Category: "rendered_difference",
					Detail: "field rendering differs from source; original prose remains in the named raw record or this report",
					Source: &SourceDetail{documentName(doc), f.Start, f.End, strings.Count(raw[:f.Start], "\n") + 1, f.Paths, original, rendered},
				})
			}
		}
	}
	// Every explicitly raw record, including duplicate approval/state originals,
	// is enumerated. No Markdown catch-all can hide prose from this inventory.
	for i, v := range p.req.Texts {
		if v.TextKind == "import_raw" || v.TextKind == "note" {
			p.rawFinding(v.DebtID, pointer("debt_text", i, "body"), v.Body, v.TextKind)
		}
		if v.Heading != "" {
			p.rawFinding(v.DebtID, pointer("debt_text", i, "heading"), v.Heading, "heading")
		}
	}
	for i, v := range p.req.SourceTexts {
		p.rawFinding("document", pointer("source_text", i, "body"), v.Body, v.TextKind)
	}
	return nil
}
func (p *parser) rawFinding(item, path, body, kind string) {
	p.report.Findings = append(p.report.Findings, Finding{Item: item, Rule: 3, Category: "raw_" + kind, Detail: "stored verbatim: " + path,
		Source: &SourceDetail{Paths: []string{path}, Original: body, Rendered: body}})
}
