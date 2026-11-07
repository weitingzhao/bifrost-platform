package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reading a plan (W-31 2.2b, the thin chief of staff). A planner hands in a
// report that ends in one fenced JSON block in the format its spec gives
// (goal.go); ParsePlan reads the last such block and checks it by program
// (ADR §12.2 item 13: the model proposes, the program lets it through).
// Nothing a planner writes reaches a command line as a program to run: a
// check of its own is one shape of `go test` (review 216 A1), and every card
// field fits the question store's own limits before the first card is made
// (review 216 B2).

// Bounds of a plan.
const (
	PlanMaxTasks     = 8
	PlanMaxQuestions = 3
	PlanMaxDecisions = 10
	PlanMaxCriteria  = 10
	planMaxVerify    = 5    // checks a plan adds to a task
	planMaxText      = 8000 // runes of a task's text
	planMaxField     = 1000 // runes of a criterion or of a decision's field
	planMaxRef       = 40
	planMaxPattern   = 200
)

// The question store's limits on a card, in bytes of the field's JSON
// encoding with its quotes (questions.encodedBytes): the same measure, so a
// card that passes here is not refused there for its length.
const (
	cardTitleBytes     = 300
	cardBodyBytes      = 3000
	cardLabelBytes     = 160
	cardMeanwhileBytes = 300
)

// PlanDoc is a plan as a planner hands it in.
type PlanDoc struct {
	Mission   PlannedMission    `json:"mission"`
	Tasks     []PlannedTask     `json:"tasks"`
	Questions []PlannedQuestion `json:"questions"`
	Decisions []PlannedDecision `json:"decisions"`

	// Ignored are the keys the plan named that a plan does not have, as
	// tasks[0].goal: read past, not refused, so a planner's spec that asks
	// for more than the reader uses costs no round.
	Ignored []string `json:"-"`
	// Notes are the slips of form check fixed or dropped, in words a planner
	// can act on: the plan's accepted event records them.
	Notes []string `json:"-"`
}

// PlannedMission is what a plan says about its mission; empty keeps what the
// mission has.
type PlannedMission struct {
	Success      []string `json:"success"`
	OutOfScope   []string `json:"out_of_scope"`
	MaxAttempts  int      `json:"max_attempts"`
	MaxWallHours int      `json:"max_wall_hours"`
}

// PlannedTask is one implementation task of a plan. Core marks it core work
// (core.go): true or false, false when left out; kept as written so that a
// null or another type is refused, not read as false.
type PlannedTask struct {
	Ref       string          `json:"ref"`
	Title     string          `json:"title"`
	Repo      string          `json:"repo"`
	Text      string          `json:"text"`
	Verify    [][]string      `json:"verify"`
	DependsOn []string        `json:"depends_on"`
	Core      json.RawMessage `json:"core,omitempty"`

	// Siblings are the goal's repositories the task's text names beside its
	// checkout (../<repo>) that its repository does not have: check adds
	// them, and the task's spec has them beside the checkout at main.
	Siblings []string `json:"-"`
}

// IsCore says whether a checked task is core work.
func (t PlannedTask) IsCore() bool { return string(t.Core) == "true" }

// PlannedQuestion is a decision card for the Owner.
type PlannedQuestion struct {
	Question    string   `json:"question"`
	Body        string   `json:"body"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
	Meanwhile   string   `json:"meanwhile"`
	OwnerDuty   string   `json:"owner_duty"`
}

// PlannedDecision is a choice the planner made.
type PlannedDecision struct {
	Decision string `json:"decision"`
	Why      string `json:"why"`
	Dropped  string `json:"dropped"`
	Reverse  string `json:"reverse"`
}

var planFence = regexp.MustCompile("(?s)```json[ \\t]*\\r?\\n(.*?)\\r?\\n[ \\t]*```")

// ParsePlan reads the last fenced json block of a planner's report and checks
// it; repos are the repositories its mission names. A slip of form comes
// back fixed or dropped, named in Notes; an error, for a plan that cannot be
// used, says what is wrong in words a planner can act on: it goes into the
// next round's spec.
func ParsePlan(report []byte, repos []string) (PlanDoc, error) {
	d, err := planBlock(report)
	if err != nil {
		return d, err
	}
	err = d.check(repos)
	return d, err
}

// PlanBlock says whether a report ends in a fenced json block that reads as a
// plan, before any check of its content: what a planner's own session can
// still fix in the same attempt (agentd planfix.go). Its errors are
// ParsePlan's.
func PlanBlock(report []byte) error {
	_, err := planBlock(report)
	return err
}

// planBlock decodes the plan block of a report (lastPlanBlock). A key a plan
// does not have is read past and named in Ignored; a key named twice is
// still refused.
func planBlock(report []byte) (PlanDoc, error) {
	var d PlanDoc
	block := lastPlanBlock(report)
	if block == nil {
		return d, errors.New("the report has no ```json block")
	}
	if err := oneOfEachKey(block); err != nil {
		return d, err
	}
	dec := json.NewDecoder(bytes.NewReader(block))
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("the plan does not read: %v", err)
	}
	if dec.More() {
		return d, errors.New("the plan block holds more than one value")
	}
	d.Ignored = ignoredKeys(block)
	return d, nil
}

// lastPlanBlock is the content of the last ```json block of a report; with
// none, of the last block fenced ```JSON or ``` with no language whose
// content starts with {, as a planner that forgot the language writes it;
// nil with neither.
func lastPlanBlock(report []byte) []byte {
	if blocks := planFence.FindAllSubmatch(report, -1); len(blocks) > 0 {
		return blocks[len(blocks)-1][1]
	}
	var last []byte
	lines := bytes.SplitAfter(report, []byte("\n"))
	for i := 0; i < len(lines); i++ {
		open := bytes.TrimSpace(lines[i])
		if !bytes.HasPrefix(open, []byte("```")) {
			continue
		}
		lang := string(bytes.TrimSpace(open[3:]))
		end := i + 1
		for end < len(lines) && string(bytes.TrimSpace(lines[end])) != "```" {
			end++
		}
		if end == len(lines) {
			break // a fence never closed holds no block
		}
		content := bytes.Join(lines[i+1:end], nil)
		if (lang == "" || lang == "JSON") && bytes.HasPrefix(bytes.TrimSpace(content), []byte("{")) {
			last = content
		}
		i = end
	}
	return last
}

// planIgnoredMax is how many ignored keys a used plan's reason names.
const planIgnoredMax = 5

// ignoredKeys names the keys of a plan block that PlanDoc does not have: at
// the top level, in mission and in each task, question and decision, in that
// order. A key matches a field whatever its case, as the decoder matches it.
func ignoredKeys(block []byte) []string {
	var ignored []string
	unknown := func(at string, raw json.RawMessage, of any) {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		known := jsonKeys(of)
		keys := make([]string, 0, len(m))
		for k := range m {
			if !known[strings.ToLower(k)] {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			if at != "" {
				k = at + "." + k
			}
			ignored = append(ignored, k)
		}
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(block, &top) != nil {
		return nil
	}
	unknown("", block, PlanDoc{})
	field := func(name string) json.RawMessage {
		for k, v := range top {
			if strings.EqualFold(k, name) {
				return v
			}
		}
		return nil
	}
	if m := field("mission"); m != nil {
		unknown("mission", m, PlannedMission{})
	}
	for _, list := range []struct {
		name string
		of   any
	}{{"tasks", PlannedTask{}}, {"questions", PlannedQuestion{}}, {"decisions", PlannedDecision{}}} {
		var items []json.RawMessage
		if json.Unmarshal(field(list.name), &items) != nil {
			continue
		}
		for i, it := range items {
			unknown(fmt.Sprintf("%s[%d]", list.name, i), it, list.of)
		}
	}
	return ignored
}

// jsonKeys are the lower-cased JSON keys of a struct's fields.
func jsonKeys(v any) map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[strings.ToLower(name)] = true
		}
	}
	return keys
}

// oneOfEachKey refuses a plan in which one object names a key twice: Go's
// decoder keeps the last, and matches a key to a field whatever its case, so
// "core": true then "Core": false would read as false (review 426 C2). A
// block that does not read is left to the decoder to say why.
func oneOfEachKey(block []byte) error {
	dec := json.NewDecoder(bytes.NewReader(block))
	type frame struct {
		object bool
		keys   map[string]bool
		key    bool // the next token is a key
	}
	var stack []*frame
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.object && top.key {
			if k, ok := tok.(string); ok {
				if top.keys[strings.ToLower(k)] {
					return fmt.Errorf("the plan names %q twice in one object", cutBytes(k, 40))
				}
				top.keys[strings.ToLower(k)] = true
				top.key = false
				continue
			}
		}
		switch tok {
		case json.Delim('{'):
			stack = append(stack, &frame{object: true, keys: map[string]bool{}, key: true})
			continue
		case json.Delim('['):
			stack = append(stack, &frame{})
			continue
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && stack[len(stack)-1].object {
			stack[len(stack)-1].key = true // a value ended: a key comes next
		}
	}
}

// check makes a plan usable as it is used, or says why it cannot be: it
// refuses only a plan the runtime cannot carry out (no task, or more than
// PlanMaxTasks; a ref used twice; depends_on naming no other task of the
// plan; a dependency cycle; a task in, or naming beside it, a repository the
// goal does not cover). Every other slip of form is fixed or dropped and
// named in d.Notes, which the plan's accepted event records so the planner
// sees it (the Owner, 2026-11-06: a refusal names the risk it prevents).
func (d *PlanDoc) check(repos []string) error {
	note := d.note
	m := &d.Mission
	m.Success = d.fixTexts("mission.success", m.Success, PlanMaxCriteria, planMaxField)
	m.OutOfScope = d.fixTexts("mission.out_of_scope", m.OutOfScope, PlanMaxCriteria, planMaxField)
	if m.MaxAttempts < 0 {
		note("mission.max_attempts %d dropped", m.MaxAttempts)
		m.MaxAttempts = 0
	}
	if m.MaxWallHours < 0 {
		note("mission.max_wall_hours %d dropped", m.MaxWallHours)
		m.MaxWallHours = 0
	}
	if len(d.Tasks) == 0 || len(d.Tasks) > PlanMaxTasks {
		return fmt.Errorf("a plan has 1 to %d tasks, not %d", PlanMaxTasks, len(d.Tasks))
	}
	refs := map[string]bool{}
	for i := range d.Tasks {
		t := &d.Tasks[i]
		at := fmt.Sprintf("tasks[%d]", i)
		if ref := fixRef(t.Ref); ref != t.Ref || ref == "" {
			if ref == "" {
				ref = fmt.Sprintf("t%d", i+1)
			}
			note("%s: ref %q read as %q", at, cutBytes(t.Ref, 40), ref)
			t.Ref = ref
		}
		for j, on := range t.DependsOn {
			t.DependsOn[j] = fixRef(on)
		}
		switch {
		case refs[t.Ref]:
			return fmt.Errorf("%s: ref %q is used twice", at, t.Ref)
		case !slices.Contains(repos, t.Repo):
			return fmt.Errorf("%s: repo is one of %s, not %q", at, strings.Join(repos, ", "), cutBytes(t.Repo, 80))
		}
		refs[t.Ref] = true
		t.Text = d.fixText(at+".text", t.Text, planMaxText, true)
		t.Title = d.fixText(at+".title", t.Title, maxTaskTitle, false)
		if t.Title == "" {
			first, _, _ := strings.Cut(t.Text, "\n")
			if t.Title = cutRunes(cleanText(first, false), maxTaskTitle); t.Title == "" {
				t.Title = t.Ref
			}
			note("%s: the empty title takes %q", at, t.Title)
		}
		if t.Text == "" {
			t.Text = t.Title
			note("%s: the empty text takes the title", at)
		}
		t.Verify = d.fixVerify(at, t.Verify, Repos[t.Repo].TestDirs)
		for _, name := range outsideCheckout(t.Text, t.Repo) {
			switch {
			case slices.Contains(t.Siblings, name):
			case !slices.Contains(repos, name):
				return fmt.Errorf("%s: its text names ../%s, which is not a repository this goal covers (a task in %s has %s); "+
					"what needs %s is no part of this plan", at, name, t.Repo, CheckoutOf(t.Repo), name)
			case len(Repos[t.Repo].Siblings)+len(t.Siblings) >= planMaxSiblings:
				note("%s: ../%s not added beside its checkout: a task has at most %d repositories beside it", at, name, planMaxSiblings)
			default:
				t.Siblings = append(t.Siblings, name)
				note("%s: its text names ../%s, added beside its checkout at the head of main", at, name)
			}
		}
		var why string
		if t.Core, why = fixPlannedCore(at, *t); why != "" {
			note("%s", why)
		}
	}
	for i, t := range d.Tasks {
		for _, on := range t.DependsOn {
			if !refs[on] || on == t.Ref {
				return fmt.Errorf("tasks[%d]: depends_on names %q, which is not another task of this plan", i, cutBytes(on, 40))
			}
		}
	}
	if cyc := dependencyCycle(d.Tasks); cyc != "" {
		return fmt.Errorf("the tasks' depends_on make a cycle through %s", cyc)
	}
	if len(d.Questions) > PlanMaxQuestions {
		note("questions after the first %d dropped", PlanMaxQuestions)
		d.Questions = d.Questions[:PlanMaxQuestions]
	}
	questions := []PlannedQuestion{}
	for i, q := range d.Questions {
		if q, ok := d.fixQuestion(fmt.Sprintf("questions[%d]", i), q); ok {
			questions = append(questions, q)
		}
	}
	d.Questions = questions
	if len(d.Decisions) > PlanMaxDecisions {
		note("decisions after the first %d dropped", PlanMaxDecisions)
		d.Decisions = d.Decisions[:PlanMaxDecisions]
	}
	decisions := []PlannedDecision{}
	for i, c := range d.Decisions {
		at := fmt.Sprintf("decisions[%d]", i)
		if c.Decision = d.fixText(at+".decision", c.Decision, planMaxField, true); c.Decision == "" {
			note("%s dropped: no decision", at)
			continue
		}
		if c.Why = d.fixText(at+".why", c.Why, planMaxField, true); c.Why == "" {
			c.Why = "not given"
			note("%s: no why given", at)
		}
		c.Dropped = d.fixText(at+".dropped", c.Dropped, planMaxField, true)
		c.Reverse = d.fixText(at+".reverse", c.Reverse, planMaxField, true)
		decisions = append(decisions, c)
	}
	d.Decisions = decisions
	return nil
}

// planMaxSiblings is how many repositories a task has beside its checkout,
// as agentd takes a spec's siblings.
const planMaxSiblings = 4

func (d *PlanDoc) note(format string, a ...any) { d.Notes = append(d.Notes, fmt.Sprintf(format, a...)) }

// fixQuestion makes a question a card the question store takes: each field
// cut to fit its limit, at most 4 options, a recommended and an owner_duty
// it knows, a meanwhile; false drops a question with no question or with
// fewer than 2 options.
func (d *PlanDoc) fixQuestion(at string, q PlannedQuestion) (PlannedQuestion, bool) {
	if q.Question = d.fixCard(at+".question", q.Question, cardTitleBytes, false); q.Question == "" {
		d.note("%s dropped: no question", at)
		return q, false
	}
	q.Body = d.fixCard(at+".body", q.Body, cardBodyBytes, true)
	options := []string{}
	for j, o := range q.Options {
		if o = d.fixCard(fmt.Sprintf("%s.options[%d]", at, j), o, cardLabelBytes, false); o != "" {
			options = append(options, o)
		}
	}
	if len(options) > 4 {
		d.note("%s: options after the first 4 dropped", at)
		options = options[:4]
	}
	if len(options) < 2 {
		d.note("%s dropped: %d options, a question has 2 to 4", at, len(options))
		return q, false
	}
	q.Options = options
	if q.Recommended < 1 || q.Recommended > len(q.Options) {
		d.note("%s: recommended %d read as 1", at, q.Recommended)
		q.Recommended = 1
	}
	if !slices.Contains([]string{"goal", "rule", "irreversible"}, q.OwnerDuty) {
		d.note("%s: owner_duty %q read as goal", at, cutBytes(q.OwnerDuty, 40))
		q.OwnerDuty = "goal"
	}
	if q.Meanwhile = d.fixCard(at+".meanwhile", q.Meanwhile, cardMeanwhileBytes, false); q.Meanwhile == "" {
		q.Meanwhile = cutEncoded("the work goes on with "+q.Options[q.Recommended-1], cardMeanwhileBytes)
		d.note("%s: the empty meanwhile takes the recommended option", at)
	}
	return q, true
}

// fixVerify keeps the checks of a task that checkTest admits, at most
// planMaxVerify; the others are dropped, the task kept.
func (d *PlanDoc) fixVerify(at string, verify [][]string, dirs []string) [][]string {
	if len(verify) > 0 && len(dirs) == 0 {
		d.note("%s.verify dropped: this repository takes no checks of a plan's own", at)
		return nil
	}
	kept := [][]string{}
	for j, c := range verify {
		if err := checkTest(fmt.Sprintf("%s.verify[%d]", at, j), c, dirs); err != nil {
			d.note("%s; dropped", cutBytes(err.Error(), 200))
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) > planMaxVerify {
		d.note("%s.verify: checks after the first %d dropped", at, planMaxVerify)
		kept = kept[:planMaxVerify]
	}
	return kept
}

// fixRef is a ref with only letters, digits, - and _, at most planMaxRef.
func fixRef(ref string) string {
	ref = strings.Map(func(r rune) rune {
		if notRefRune(r) {
			return -1
		}
		return r
	}, ref)
	if len(ref) > planMaxRef {
		ref = ref[:planMaxRef]
	}
	return ref
}

// fixText is s with its control characters stripped (cleanText), trimmed
// and cut to at most max runes with "…"; each fix is noted.
func (d *PlanDoc) fixText(at, s string, max int, lines bool) string {
	c := cleanText(s, lines)
	if c != strings.TrimSpace(s) {
		d.note("%s: control characters stripped", at)
	}
	if utf8.RuneCountInString(c) > max {
		d.note("%s cut to %d characters", at, max)
		c = cutRunes(c, max)
	}
	return c
}

// fixCard is fixText by the question store's measure: at most maxBytes bytes
// of its JSON encoding, quotes included.
func (d *PlanDoc) fixCard(at, s string, maxBytes int, lines bool) string {
	c := cleanText(s, lines)
	if c != strings.TrimSpace(s) {
		d.note("%s: control characters stripped", at)
	}
	if encodedLen(c) > maxBytes {
		d.note("%s cut to %d bytes encoded, the question store's limit", at, maxBytes)
		c = cutEncoded(c, maxBytes)
	}
	return c
}

// fixTexts keeps the first most texts of a list, each fixed as fixText
// does, the empty ones dropped.
func (d *PlanDoc) fixTexts(at string, list []string, most, max int) []string {
	if len(list) > most {
		d.note("%s after the first %d dropped", at, most)
		list = list[:most]
	}
	var kept []string
	for i, s := range list {
		if s = d.fixText(fmt.Sprintf("%s[%d]", at, i), s, max, false); s != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) < len(list) {
		d.note("%s: %d empty dropped", at, len(list)-len(kept))
	}
	return kept
}

// cleanText is s trimmed, with no control character but, where lines are
// allowed, a line break or a tab; where they are not, a line break or a tab
// reads as a space.
func cleanText(s string, lines bool) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case !unicode.IsControl(r):
			return r
		case lines && (r == '\n' || r == '\t'):
			return r
		case lines && r == '\r':
			return -1
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		}
		return -1
	}, s)
	return strings.TrimSpace(s)
}

// cutEncoded cuts s to the most runes whose JSON encoding, with "…" when
// cut, is at most maxBytes bytes.
func cutEncoded(s string, maxBytes int) string {
	if encodedLen(s) <= maxBytes {
		return s
	}
	r := []rune(s)
	lo, hi := 0, len(r) // r[:lo] with "…" fits; r[:hi] with "…" does not, or is all of s
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if encodedLen(string(r[:mid])+"…") <= maxBytes {
			lo = mid
		} else {
			hi = mid
		}
	}
	return strings.TrimRightFunc(string(r[:lo]), unicode.IsSpace) + "…"
}

// siblingPath is a path to a repository beside a checkout, as a planner that
// read the goal's repositories side by side writes it: ../bifrost-<name>.
var siblingPath = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])\.\./(bifrost-[A-Za-z0-9_-]*[A-Za-z0-9])`)

// outsideCheckout are the repositories a task's text names by a path beside
// its checkout that the checkout does not have (CheckoutOf), each once.
// It is the part of "a task works in its own checkout" a program can see; a
// text that asks for another repository in words ("on the infra tree", T26's
// first run) reads like any other, and the planner's spec says what a task
// has (goal.go checkouts).
func outsideCheckout(text, repo string) []string {
	var names []string
	for _, m := range siblingPath.FindAllStringSubmatch(text, -1) {
		if name := m[1]; name != repo && !inCheckout(repo, name) && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

var (
	testPackage = regexp.MustCompile(`^\./[A-Za-z0-9_/.-]*$`)
	testPattern = regexp.MustCompile(`^[A-Za-z0-9_|^$.*+?()\[\]/][A-Za-z0-9_|^$.*+?()\[\]/-]*$`)
)

// checkTest admits one shape of check, a test run of packages of the module
// in dirs: go -C <dir> test [-run <pattern>] ./<package>... No other
// subcommand (run, generate), no other flag (-exec, -toolexec, -o, -C
// elsewhere), no path out of the module.
func checkTest(at string, c []string, dirs []string) error {
	if len(dirs) == 0 {
		return fmt.Errorf("%s: this repository takes no checks of a plan's own; leave verify empty", at)
	}
	bad := func(why string) error {
		return fmt.Errorf("%s: a check is go -C <%s> test [-run <pattern>] ./<package>...; %s",
			at, strings.Join(dirs, "|"), why)
	}
	if len(c) < 5 || c[0] != "go" || c[1] != "-C" || !slices.Contains(dirs, c[2]) || c[3] != "test" {
		return bad("this one is " + strings.Join(c, " "))
	}
	args := c[4:]
	if args[0] == "-run" {
		if len(args) < 3 || len(args[1]) > planMaxPattern || !testPattern.MatchString(args[1]) {
			return bad("-run takes one test name pattern")
		}
		args = args[2:]
	}
	for _, p := range args {
		if !testPackage.MatchString(p) || slices.Contains(strings.Split(p, "/"), "..") {
			return bad(fmt.Sprintf("%q is not a package path of the module", p))
		}
	}
	return nil
}

func notRefRune(r rune) bool {
	return !(r == '-' || r == '_' || r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)))
}

// encodedLen is the length of s's JSON encoding without HTML escaping, quotes
// included, as the question store measures it.
func encodedLen(s string) int {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return len(s) + 2
	}
	return len(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// dependencyCycle names a task on a cycle of depends_on, or "".
func dependencyCycle(tasks []PlannedTask) string {
	deps := map[string][]string{}
	for _, t := range tasks {
		deps[t.Ref] = t.DependsOn
	}
	state := map[string]int{} // 1 on the path, 2 done
	var visit func(string) string
	visit = func(ref string) string {
		switch state[ref] {
		case 1:
			return ref
		case 2:
			return ""
		}
		state[ref] = 1
		for _, on := range deps[ref] {
			if c := visit(on); c != "" {
				return c
			}
		}
		state[ref] = 2
		return ""
	}
	for _, t := range tasks {
		if c := visit(t.Ref); c != "" {
			return c
		}
	}
	return ""
}
