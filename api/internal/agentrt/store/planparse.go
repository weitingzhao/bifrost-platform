package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
// it; repos are the repositories its mission names. An error says what is
// wrong in words a planner can act on: it goes into the next round's spec.
func ParsePlan(report []byte, repos []string) (PlanDoc, error) {
	var d PlanDoc
	blocks := planFence.FindAllSubmatch(report, -1)
	if len(blocks) == 0 {
		return d, errors.New("the report has no ```json block")
	}
	dec := json.NewDecoder(bytes.NewReader(blocks[len(blocks)-1][1]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("the plan does not read: %v", err)
	}
	if dec.More() {
		return d, errors.New("the plan block holds more than one value")
	}
	return d, d.check(repos)
}

func (d PlanDoc) check(repos []string) error {
	m := d.Mission
	if err := texts("mission.success", m.Success, PlanMaxCriteria, planMaxField); err != nil {
		return err
	}
	if err := texts("mission.out_of_scope", m.OutOfScope, PlanMaxCriteria, planMaxField); err != nil {
		return err
	}
	if m.MaxAttempts < 0 || m.MaxWallHours < 0 {
		return errors.New("mission: a budget is not negative")
	}
	if len(d.Tasks) == 0 || len(d.Tasks) > PlanMaxTasks {
		return fmt.Errorf("a plan has 1 to %d tasks, not %d", PlanMaxTasks, len(d.Tasks))
	}
	refs := map[string]bool{}
	for i, t := range d.Tasks {
		at := fmt.Sprintf("tasks[%d]", i)
		switch {
		case t.Ref == "" || len(t.Ref) > planMaxRef || strings.IndexFunc(t.Ref, notRefRune) >= 0:
			return fmt.Errorf("%s: ref is 1 to %d letters, digits, - or _", at, planMaxRef)
		case refs[t.Ref]:
			return fmt.Errorf("%s: ref %q is used twice", at, t.Ref)
		case !slices.Contains(repos, t.Repo):
			return fmt.Errorf("%s: repo is one of %s, not %q", at, strings.Join(repos, ", "), t.Repo)
		case len(t.Verify) > planMaxVerify:
			return fmt.Errorf("%s: at most %d checks of its own", at, planMaxVerify)
		}
		refs[t.Ref] = true
		if err := text(at+".title", t.Title, maxTaskTitle, false); err != nil {
			return err
		}
		if err := text(at+".text", t.Text, planMaxText, true); err != nil {
			return err
		}
		for j, c := range t.Verify {
			if err := checkTest(fmt.Sprintf("%s.verify[%d]", at, j), c, Repos[t.Repo].TestDirs); err != nil {
				return err
			}
		}
		if name := outsideCheckout(t.Text, t.Repo); name != "" {
			return fmt.Errorf("%s: its text names ../%s, which a task in %s does not have (it has %s); what needs %s is a task "+
				"in %s, or no part of this plan", at, name, t.Repo, CheckoutOf(t.Repo), name, name)
		}
		if err := checkPlannedCore(at, t); err != nil {
			return err
		}
	}
	for i, t := range d.Tasks {
		for _, on := range t.DependsOn {
			if !refs[on] || on == t.Ref {
				return fmt.Errorf("tasks[%d]: depends_on names %q, which is not another task of this plan", i, on)
			}
		}
	}
	if cyc := dependencyCycle(d.Tasks); cyc != "" {
		return fmt.Errorf("the tasks' depends_on make a cycle through %s", cyc)
	}
	if len(d.Questions) > PlanMaxQuestions {
		return fmt.Errorf("a plan asks the Owner at most %d questions", PlanMaxQuestions)
	}
	for i, q := range d.Questions {
		at := fmt.Sprintf("questions[%d]", i)
		if err := cardText(at+".question", q.Question, cardTitleBytes, false); err != nil {
			return err
		}
		if q.Body != "" {
			if err := cardText(at+".body", q.Body, cardBodyBytes, true); err != nil {
				return err
			}
		}
		if len(q.Options) < 2 || len(q.Options) > 4 {
			return fmt.Errorf("%s: 2 to 4 options", at)
		}
		for j, o := range q.Options {
			if err := cardText(fmt.Sprintf("%s.options[%d]", at, j), o, cardLabelBytes, false); err != nil {
				return err
			}
		}
		switch {
		case q.Recommended < 1 || q.Recommended > len(q.Options):
			return fmt.Errorf("%s: recommended is the 1-based option the work goes on with", at)
		case !slices.Contains([]string{"goal", "rule", "irreversible"}, q.OwnerDuty):
			return fmt.Errorf("%s: owner_duty is goal, rule or irreversible", at)
		}
		if err := cardText(at+".meanwhile", q.Meanwhile, cardMeanwhileBytes, false); err != nil {
			return err
		}
	}
	if len(d.Decisions) > PlanMaxDecisions {
		return fmt.Errorf("a plan records at most %d decisions", PlanMaxDecisions)
	}
	for i, c := range d.Decisions {
		at := fmt.Sprintf("decisions[%d]", i)
		if err := text(at+".decision", c.Decision, planMaxField, true); err != nil {
			return err
		}
		if err := text(at+".why", c.Why, planMaxField, true); err != nil {
			return err
		}
		for name, v := range map[string]string{"dropped": c.Dropped, "reverse": c.Reverse} {
			if v != "" {
				if err := text(at+"."+name, v, planMaxField, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// siblingPath is a path to a repository beside a checkout, as a planner that
// read the goal's repositories side by side writes it: ../bifrost-<name>.
var siblingPath = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])\.\./(bifrost-[A-Za-z0-9_-]*[A-Za-z0-9])`)

// outsideCheckout is the first repository a task's text names by a path
// beside its checkout that the checkout does not have (CheckoutOf), or "".
// It is the part of "a task works in its own checkout" a program can see; a
// text that asks for another repository in words ("on the infra tree", T26's
// first run) reads like any other, and the planner's spec says what a task
// has (goal.go checkouts).
func outsideCheckout(text, repo string) string {
	for _, m := range siblingPath.FindAllStringSubmatch(text, -1) {
		if name := m[1]; name != repo && !inCheckout(repo, name) {
			return name
		}
	}
	return ""
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

// text checks one string: present, at most max runes, no control character
// but a line break or a tab where lines are allowed.
func text(at, s string, max int, lines bool) error {
	switch {
	case strings.TrimSpace(s) == "":
		return fmt.Errorf("%s is empty", at)
	case utf8.RuneCountInString(s) > max:
		return fmt.Errorf("%s is longer than %d characters", at, max)
	}
	return controls(at, s, lines)
}

// cardText checks a card's field by the question store's measure: at most
// maxBytes bytes of its JSON encoding, quotes included.
func cardText(at, s string, maxBytes int, lines bool) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is empty", at)
	}
	if n := encodedLen(s); n > maxBytes {
		return fmt.Errorf("%s is %d bytes encoded; the question store takes at most %d", at, n, maxBytes)
	}
	return controls(at, s, lines)
}

func controls(at, s string, lines bool) error {
	for _, c := range s {
		if unicode.IsControl(c) && !(lines && (c == '\n' || c == '\t')) {
			return fmt.Errorf("%s has a control character %U", at, c)
		}
	}
	return nil
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

func texts(at string, list []string, most, max int) error {
	if len(list) > most {
		return fmt.Errorf("%s holds at most %d", at, most)
	}
	for i, s := range list {
		if err := text(fmt.Sprintf("%s[%d]", at, i), s, max, false); err != nil {
			return err
		}
	}
	return nil
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
