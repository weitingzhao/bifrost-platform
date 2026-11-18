package instruments

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const lawsHeading = "## 登记"

var (
	lawColumns = []string{"#", "哪天", "他的话（要点）", "落在哪", "机器强制", "状态", "旧说法"}
	lawNumber  = regexp.MustCompile(`^H?[0-9]+$`)
	ruleCell   = regexp.MustCompile(`^:?-+:?$`)
)

// tableCells splits a Markdown table row; "\|" is a pipe inside a cell.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cell.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(line[i])
		}
	}
	if rest := strings.TrimSpace(cell.String()); rest != "" {
		cells = append(cells, rest)
	}
	return cells
}

func sameCells(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func lawState(status string) string {
	switch {
	case strings.HasPrefix(status, "已落"):
		return "landed"
	case strings.HasPrefix(status, "未落"):
		return "open"
	case strings.HasPrefix(status, "不落"):
		return "not_applicable"
	}
	return "unknown"
}

// machineEnforced reads the 机器强制 cell; "—" is how the register writes an
// empty cell.
func machineEnforced(cell string) bool {
	switch cell {
	case "", "无", "—", "-":
		return false
	}
	return !strings.HasPrefix(cell, "只有文字") && !strings.HasPrefix(cell, "没有")
}

// parseLaws reads the table under the register heading. The columns are read
// by position, so a table whose header changed is not read at all.
func parseLaws(r *reading, text string) []Law {
	source := sourceName(infraRepo, lawsPath)
	laws := []Law{}
	inSection, headerSeen := false, false
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == lawsHeading {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := tableCells(trimmed)
		if !headerSeen {
			if !sameCells(cells, lawColumns) {
				r.malformed(source, "the register's columns are not the expected ones; the table was not read")
				return []Law{}
			}
			headerSeen = true
			continue
		}
		if len(cells) > 0 && ruleCell.MatchString(cells[0]) {
			continue
		}
		at := fmt.Sprintf("%s#L%d", source, i+1)
		if len(cells) != len(lawColumns) {
			r.malformed(at, fmt.Sprintf("the row has %d columns, not %d", len(cells), len(lawColumns)))
			continue
		}
		law := Law{Number: cells[0], Date: cells[1], Text: cells[2], Where: cells[3], Enforcement: cells[4],
			Status: cells[5], State: lawState(cells[5]), MachineEnforced: machineEnforced(cells[4]), Skills: []string{}}
		if !lawNumber.MatchString(law.Number) {
			r.malformed(at, "the row's number is not a law number")
		}
		if seen[law.Number] {
			r.malformed(at, "law "+law.Number+" is registered twice; the first row is shown")
			continue
		}
		seen[law.Number] = true
		if law.State == "unknown" {
			r.malformed(at, "the status starts with none of 已落, 未落, 不落")
		}
		laws = append(laws, law)
	}
	if !headerSeen {
		r.malformed(source, "no register table under "+lawsHeading)
	}
	return laws
}

// A citation is 法 29 or 法 29、33, and OWNER_LAW #30 or #30 beside OWNER_LAW.
// 法 closing another word (做法 2, 办法 3) is not one, so before it may stand
// only what is not a Han character or one of the words that lead to a law.
var (
	lawWord      = regexp.MustCompile(`法\s*(H?[0-9]+(?:\s*[、,，和与及/]\s*H?[0-9]+)*)`)
	afterLawName = regexp.MustCompile(`OWNER_LAW(?:\.md)?[^\n#]{0,12}#(H?[0-9]+(?:\s*[、,，和与及/]\s*#?H?[0-9]+)*)`)
	beforeName   = regexp.MustCompile(`#(H?[0-9]+)[^\n#0-9]{0,12}OWNER_LAW`)
	numberInList = regexp.MustCompile(`H?[0-9]+`)
	leadsToLaw   = "的按照依据见与和及"
)

func isHan(c rune) bool { return c >= 0x4e00 && c <= 0x9fff }

func citedLaws(text string) []string {
	found := map[string]bool{}
	add := func(list string) {
		for _, n := range numberInList.FindAllString(list, -1) {
			found[n] = true
		}
	}
	for _, m := range lawWord.FindAllStringSubmatchIndex(text, -1) {
		before := []rune(text[:m[0]])
		if len(before) > 0 {
			last := before[len(before)-1]
			if isHan(last) && !strings.ContainsRune(leadsToLaw, last) {
				continue
			}
		}
		if m[1] < len(text) && text[m[1]] == '-' {
			continue // a date or a range of something else
		}
		add(text[m[2]:m[3]])
	}
	for _, m := range afterLawName.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range beforeName.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	out := make([]string, 0, len(found))
	for n := range found {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return lawLess(out[i], out[j]) })
	return out
}

// lawLess orders the numbered laws first, then the H series.
func lawLess(a, b string) bool {
	ah, bh := strings.HasPrefix(a, "H"), strings.HasPrefix(b, "H")
	if ah != bh {
		return bh
	}
	an, _ := strconv.Atoi(strings.TrimPrefix(a, "H"))
	bn, _ := strconv.Atoi(strings.TrimPrefix(b, "H"))
	return an < bn
}

func skillNameChar(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// namesSkill says whether text names the skill: skills/<name>, or the bare
// name as a word of its own. A bare name inside a path is another thing with
// the same name (scripts/code-health/scan.sh is not the code-health skill).
func namesSkill(text, name string) bool {
	if name == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], name)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(name)
		from = end
		if end < len(text) && skillNameChar(text[end]) {
			continue
		}
		if strings.HasSuffix(text[:start], "skills/") {
			return true
		}
		if start > 0 && (skillNameChar(text[start-1]) || text[start-1] == '/' || text[start-1] == '.') {
			continue
		}
		if end < len(text) && text[end] == '/' {
			continue
		}
		return true
	}
}

// linkLawsAndSkills lists under each law the skills it names, and reports a
// skill citing a law the register does not have (when the register was read).
func linkLawsAndSkills(r *reading, laws []Law, skills []Skill) {
	known := map[string]bool{}
	for i := range laws {
		known[laws[i].Number] = true
		text := laws[i].Where + " " + laws[i].Enforcement
		for _, s := range skills {
			if namesSkill(text, s.Name) {
				laws[i].Skills = append(laws[i].Skills, s.Name)
			}
		}
	}
	if len(laws) == 0 {
		return
	}
	for _, s := range skills {
		for _, n := range s.Laws {
			if !known[n] {
				r.malformed(sourceName(infraRepo, s.Path), "cites law "+n+", which the register does not have")
			}
		}
	}
}
