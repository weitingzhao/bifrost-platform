package agentd

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// SelfCheck is how an implementation's executor ran the spec's hand-in
// commands in its own session (R5:5.7, the check value table), over the whole
// attempt, however many actions the tail kept: Runs results seen, Red those
// that exited non-zero, Fixed a red command's later exit 0 in the attempt,
// LastGreen whether the last result of every hand-in command that ran exited
// 0, and Seconds the time from each command's run to its result, summed.
type SelfCheck struct {
	Runs      int  `json:"runs"`
	Red       int  `json:"red"`
	Fixed     int  `json:"fixed"`
	LastGreen bool `json:"last_green"`
	Seconds   int  `json:"seconds"`
}

// selfCount counts the results of the hand-in commands for SelfCheck. It is
// fed from the log's raw command lines, before clean: what is masked for the
// platform is not what the spec printed.
type selfCount struct {
	mu      sync.Mutex
	cmds    map[string]int // a hand-in command line → its index
	runs    int
	red     int
	fixed   int
	seconds time.Duration
	started []time.Time // each command's last run without its result yet
	last    []*int      // each command's last exit code
}

// maxHandInLine bounds a command line compared with the hand-in commands:
// none the platform prints is longer.
const maxHandInLine = 4096

func newSelfCount(handIn []string) *selfCount {
	if len(handIn) == 0 {
		return nil
	}
	c := &selfCount{cmds: map[string]int{}, started: make([]time.Time, len(handIn)), last: make([]*int, len(handIn))}
	for i, s := range handIn {
		if _, twice := c.cmds[s]; !twice {
			c.cmds[s] = i
		}
	}
	return c
}

// index is which hand-in command line is, or -1.
func (c *selfCount) index(line, dir string) int {
	if c == nil || len(line) > maxHandInLine {
		return -1
	}
	if i, ok := c.cmds[handInForm(line, dir)]; ok {
		return i
	}
	return -1
}

func (c *selfCount) run(line, dir string, now time.Time) {
	i := c.index(line, dir)
	if i < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started[i] = now
}

func (c *selfCount) result(line, dir string, now time.Time, code int) {
	i := c.index(line, dir)
	if i < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs++
	if !c.started[i].IsZero() {
		if d := now.Sub(c.started[i]); d > 0 {
			c.seconds += d
		}
		c.started[i] = time.Time{}
	}
	prev := c.last[i]
	switch {
	case code != 0:
		c.red++
	case prev != nil && *prev != 0:
		c.fixed++
	}
	c.last[i] = &code
}

// check is the count so far, nil when no hand-in command has a result.
func (c *selfCount) check() *SelfCheck {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs == 0 {
		return nil
	}
	green := true
	for _, code := range c.last {
		if code != nil && *code != 0 {
			green = false
		}
	}
	return &SelfCheck{Runs: c.runs, Red: c.red, Fixed: c.fixed, LastGreen: green, Seconds: int(c.seconds.Round(time.Second) / time.Second)}
}

// shellWrap is the shell an executor runs a command line in (codex's
// `/bin/bash -lc "…"`), and cdFirst the change into the checkout before it.
var (
	shellWrap = regexp.MustCompile(`^(?:\S*/)?(?:ba|z)?sh -l?c (.+)$`)
	cdFirst   = regexp.MustCompile(`^cd (\S+) && (.+)$`)
)

// handInForm is a command line as the spec would print it: out of the shell
// it was wrapped in, without a change into the checkout first, with
// surrounding white space gone.
func handInForm(line, dir string) string {
	s := strings.TrimSpace(line)
	if m := shellWrap.FindStringSubmatch(s); m != nil {
		if inner, ok := unquoteShell(m[1]); ok {
			s = strings.TrimSpace(inner)
		}
	}
	if m := cdFirst.FindStringSubmatch(s); m != nil {
		if d := strings.Trim(m[1], `'"`); d == "." || dir != "" && strings.TrimRight(d, "/") == dir {
			s = strings.TrimSpace(m[2])
		}
	}
	return s
}

// unquoteShell reads one shell word quoted whole: in single quotes, a quote
// within written as the four characters shellLine writes, or in double
// quotes, with backslash escapes.
func unquoteShell(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	switch q := s[0]; {
	case q == '\'' && s[len(s)-1] == '\'':
		inner := strings.ReplaceAll(s[1:len(s)-1], `'\''`, "\x00")
		if strings.Contains(inner, "'") {
			return "", false
		}
		return strings.ReplaceAll(inner, "\x00", "'"), true
	case q == '"' && s[len(s)-1] == '"':
		var b strings.Builder
		inner := s[1 : len(s)-1]
		for i := 0; i < len(inner); i++ {
			switch ch := inner[i]; {
			case ch == '\\' && i+1 < len(inner) && strings.IndexByte(`"\$`+"`", inner[i+1]) >= 0:
				i++
				b.WriteByte(inner[i])
			case ch == '"':
				return "", false
			default:
				b.WriteByte(ch)
			}
		}
		return b.String(), true
	}
	return "", false
}
