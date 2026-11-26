// Package hostrecipe recognises an execution card that is exactly a
// registered node-run host recipe (LAW-6 七 ①; the compute line's
// scripts/owner-exec/node-run.sh render --tier host --recipe). The platform
// and the execution port both call Verify; only a card whose every byte that
// runs is pinned by the register matches. Anything else is not a recipe card.
//
// The register is the output of `node-run.sh recipes`:
//
//	recipe <name> <class> <node> <recipe_sha256> [<param>=<regexp> ...]
//	runtime <runtime_sha256>
//	image <image reference with digest>
//	deadline-max <seconds>
//
// A card matches when:
//   - it starts with the shebang, and every line before the HOSTCMD block is a
//     comment or one of TIER=host, NODE=<node>, DEADLINE=<1-4 digits>,
//     IMG='<image>', each of the last four exactly once, DEADLINE at most the
//     register's deadline-max (a register without deadline-max matches no
//     card);
//   - its header names a registered recipe, class, node and recipe_sha256, and
//     a registered runtime_sha256;
//   - the HOSTCMD block is the recipe's parameters, one K=V line each (exactly
//     the registered keys, each value matching its regexp, no white space),
//     then text whose sha256 (lines joined by \n, one \n added) is the
//     recipe's hash;
//   - everything after the HOSTCMD_EOF line hashes to the runtime_sha256;
//   - IMG is a registered image (a register without image lines matches no
//     card: the image the Pod enters the host with is part of what runs).
package hostrecipe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The classes a recipe may have; only these two run without the Owner's code.
const (
	ClassReadOnly   = "read-only"
	ClassReversible = "reversible"
)

// Recipe is one registered host recipe.
type Recipe struct {
	Name, Class, Node, SHA256 string
	Params                    map[string]*regexp.Regexp
}

// Register is the parsed output of `node-run.sh recipes`.
type Register struct {
	Recipes     map[string]Recipe
	Runtimes    map[string]bool
	Images      map[string]bool
	DeadlineMax int
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseRegister reads the register text. Blank lines and lines starting with
// # are skipped; any other unknown line is an error, so a format change is
// noticed rather than half-read.
func ParseRegister(text string) (*Register, error) {
	reg := &Register{Recipes: map[string]Recipe{}, Runtimes: map[string]bool{}, Images: map[string]bool{}}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "recipe":
			if len(f) < 5 || !hex64.MatchString(f[4]) || (f[2] != ClassReadOnly && f[2] != ClassReversible) {
				return nil, fmt.Errorf("register line %d: want recipe <name> <read-only|reversible> <node> <sha256> [k=re ...]", i+1)
			}
			r := Recipe{Name: f[1], Class: f[2], Node: f[3], SHA256: f[4], Params: map[string]*regexp.Regexp{}}
			for _, p := range f[5:] {
				k, v, ok := strings.Cut(p, "=")
				if !ok || k == "" || v == "" {
					return nil, fmt.Errorf("register line %d: parameter %q is not k=regexp", i+1, p)
				}
				re, err := regexp.Compile(v)
				if err != nil {
					return nil, fmt.Errorf("register line %d: parameter %s: %v", i+1, k, err)
				}
				r.Params[k] = re
			}
			if _, dup := reg.Recipes[r.Name]; dup {
				return nil, fmt.Errorf("register line %d: recipe %s listed twice", i+1, r.Name)
			}
			reg.Recipes[r.Name] = r
		case "runtime":
			if len(f) != 2 || !hex64.MatchString(f[1]) {
				return nil, fmt.Errorf("register line %d: want runtime <sha256>", i+1)
			}
			reg.Runtimes[f[1]] = true
		case "deadline-max":
			n, err := strconv.Atoi(strings.TrimSpace(strings.Join(f[1:], " ")))
			if len(f) != 2 || err != nil || n <= 0 || n > 9999 {
				return nil, fmt.Errorf("register line %d: want deadline-max <seconds, 1-9999>", i+1)
			}
			reg.DeadlineMax = n
		case "image":
			if len(f) != 2 || !strings.Contains(f[1], "@sha256:") {
				return nil, fmt.Errorf("register line %d: want image <ref@sha256:digest>", i+1)
			}
			reg.Images[f[1]] = true
		default:
			return nil, fmt.Errorf("register line %d: unknown kind %q", i+1, f[0])
		}
	}
	return reg, nil
}

// Match is what a verified card runs.
type Match struct {
	Recipe Recipe
	Params map[string]string
}

const (
	shebang    = "#!/usr/bin/env bash"
	blockStart = "read -r -d '' HOSTCMD <<'HOSTCMD_EOF'"
	blockEnd   = "HOSTCMD_EOF"
	headerMark = "# owner-exec node-run v"
)

var (
	deadlineLine = regexp.MustCompile(`^DEADLINE=[0-9]{1,4}$`)
	imgLine      = regexp.MustCompile(`^IMG='([^'\s]+)'$`)
	nodeLine     = regexp.MustCompile(`^NODE=(\S+)$`)
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Verify says whether command is exactly a registered recipe card. The error
// names the first thing that does not match.
func Verify(command string, reg *Register) (Match, error) {
	if reg == nil || len(reg.Recipes) == 0 {
		return Match{}, errors.New("no register")
	}
	if len(reg.Images) == 0 || len(reg.Runtimes) == 0 || reg.DeadlineMax == 0 {
		return Match{}, errors.New("the register pins no image, no runtime or no deadline-max")
	}
	lines := strings.Split(command, "\n")
	if len(lines) == 0 || lines[0] != shebang {
		return Match{}, errors.New("not a node-run card: no shebang")
	}
	start := -1
	for i, l := range lines {
		if l == blockStart {
			start = i
			break
		}
	}
	if start < 0 {
		return Match{}, errors.New("no HOSTCMD block")
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == blockEnd {
			end = i
			break
		}
	}
	if end < 0 {
		return Match{}, errors.New("HOSTCMD block not closed")
	}

	var header string
	seen := map[string]int{}
	var node, img string
	deadline := 0
	for _, l := range lines[1:start] {
		switch {
		case strings.HasPrefix(l, headerMark):
			if header != "" {
				return Match{}, errors.New("two node-run headers")
			}
			header = l
		case strings.HasPrefix(l, "#"):
		case l == "TIER=host":
			seen["TIER"]++
		case deadlineLine.MatchString(l):
			seen["DEADLINE"]++
			deadline, _ = strconv.Atoi(strings.TrimPrefix(l, "DEADLINE="))
		case nodeLine.MatchString(l):
			seen["NODE"]++
			node = nodeLine.FindStringSubmatch(l)[1]
		case imgLine.MatchString(l):
			seen["IMG"]++
			img = imgLine.FindStringSubmatch(l)[1]
		default:
			return Match{}, fmt.Errorf("line %q before the HOSTCMD block is not allowed", l)
		}
	}
	for _, k := range []string{"TIER", "NODE", "DEADLINE", "IMG"} {
		if seen[k] != 1 {
			return Match{}, fmt.Errorf("%s must appear exactly once before the HOSTCMD block", k)
		}
	}
	if header == "" {
		return Match{}, errors.New("no node-run header")
	}
	if deadline > reg.DeadlineMax {
		return Match{}, fmt.Errorf("DEADLINE %d is over the register's %d", deadline, reg.DeadlineMax)
	}
	h := map[string]string{}
	for _, f := range strings.Fields(strings.TrimPrefix(header, headerMark))[1:] {
		if k, v, ok := strings.Cut(f, "="); ok {
			h[k] = v
		}
	}
	rec, ok := reg.Recipes[h["recipe"]]
	if !ok {
		return Match{}, fmt.Errorf("recipe %q is not registered", h["recipe"])
	}
	switch {
	case h["tier"] != "host":
		return Match{}, errors.New("header tier is not host")
	case h["class"] != rec.Class:
		return Match{}, fmt.Errorf("header class %q, registered %q", h["class"], rec.Class)
	case h["node"] != rec.Node || node != rec.Node:
		return Match{}, fmt.Errorf("node %q/%q, registered %q", h["node"], node, rec.Node)
	case h["recipe_sha256"] != rec.SHA256:
		return Match{}, errors.New("header recipe_sha256 is not the registered one")
	case !reg.Runtimes[h["runtime_sha256"]]:
		return Match{}, errors.New("header runtime_sha256 is not registered")
	case !reg.Images[img]:
		return Match{}, fmt.Errorf("IMG %q is not registered", img)
	}

	block := lines[start+1 : end]
	n := len(rec.Params)
	if len(block) < n {
		return Match{}, errors.New("HOSTCMD block shorter than its parameters")
	}
	params := map[string]string{}
	for _, l := range block[:n] {
		k, v, ok := strings.Cut(l, "=")
		re := rec.Params[k]
		if !ok || re == nil || strings.ContainsAny(v, " \t\r") || !re.MatchString(v) {
			return Match{}, fmt.Errorf("parameter line %q is not allowed for %s", l, rec.Name)
		}
		if _, dup := params[k]; dup {
			return Match{}, fmt.Errorf("parameter %s given twice", k)
		}
		params[k] = v
	}
	if got := sum(strings.Join(block[n:], "\n") + "\n"); got != rec.SHA256 {
		return Match{}, errors.New("the recipe text does not hash to the registered recipe_sha256")
	}
	if got := sum(strings.Join(lines[end+1:], "\n")); got != h["runtime_sha256"] {
		return Match{}, errors.New("the runtime does not hash to the header's runtime_sha256")
	}
	return Match{Recipe: rec, Params: params}, nil
}

// Names lists the registered recipes, sorted (for messages and pages).
func (r *Register) Names() []string {
	out := make([]string, 0, len(r.Recipes))
	for n := range r.Recipes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
