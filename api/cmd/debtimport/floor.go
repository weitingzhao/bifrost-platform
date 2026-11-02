package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/debtimport"
)

var historicalHeading = regexp.MustCompile(`^[+-]### (TD-[1-9][0-9]*)\r?$`)
var workID = regexp.MustCompile(`\bTD-[1-9][0-9]*\b`)

func tdNumber(id string) int { n, _ := strconv.Atoi(strings.TrimPrefix(id, "TD-")); return n }

func (h *gitHistory) headingFloor() (debtimport.FloorSource, error) {
	source := debtimport.FloorSource{Kind: "infra_history_heading", Repository: filepath.Base(h.directory), Ref: "--all"}
	// Added AND removed headings preserve retired IDs, including unmerged branches.
	// Search ledger filenames at every historical location; FirstSeen is narrower.
	out, err := h.git("log", "--all", "--format=COMMIT %H", "--no-ext-diff", "--no-renames", "--unified=0", "-m", "-p", "-G^### TD-[1-9][0-9]*$", "--", "TECH_DEBT.md", ":(glob)**/TECH_DEBT.md")
	if err != nil {
		return source, err
	}
	commit := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "COMMIT ") {
			commit = strings.TrimPrefix(line, "COMMIT ")
			continue
		}
		if m := historicalHeading.FindStringSubmatch(line); m != nil && tdNumber(m[1]) > tdNumber(source.DebtID) {
			source.DebtID = m[1]
			source.Commit = commit
		}
	}
	return source, nil
}

func (h *gitHistory) mainFloors() ([]debtimport.FloorSource, error) {
	refs, err := h.git("for-each-ref", "--format=%(refname)", "refs/heads/main", "refs/remotes/origin/main")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(refs) == "" {
		return nil, fmt.Errorf("repository %s has no local main or origin/main", filepath.Base(h.directory))
	}
	result := []debtimport.FloorSource{}
	for _, ref := range strings.Fields(refs) {
		source := debtimport.FloorSource{Kind: "main_work_trailer", Repository: filepath.Base(h.directory), Ref: ref}
		// Git extracts trailers, so a TD number in prose/subject is not counted.
		out, err := h.git("log", ref, "--format=%H%x00%(trailers:key=Work,valueonly)%x00")
		if err != nil {
			return nil, err
		}
		chunks := strings.Split(out, "\x00")
		for i := 0; i+1 < len(chunks); i += 2 {
			for _, id := range workID.FindAllString(chunks[i+1], -1) {
				if tdNumber(id) > tdNumber(source.DebtID) {
					source.DebtID = id
					source.Commit = strings.TrimSpace(chunks[i])
				}
			}
		}
		result = append(result, source)
	}
	return result, nil
}

// Sibling checkouts form an explicit inventory in floor_sources; missing main
// and shallow history fail instead of silently lowering the floor.
func collectFloors(infra *gitHistory, root, reserved string) ([]debtimport.FloorSource, error) {
	heading, err := infra.headingFloor()
	if err != nil {
		return nil, err
	}
	sources := []debtimport.FloorSource{heading}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	repos := []string{infra.directory}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		if filepath.Clean(dir) != filepath.Clean(infra.directory) {
			repos = append(repos, dir)
		}
	}
	sort.Strings(repos)
	for _, dir := range repos {
		h, err := newGitHistory(dir)
		if err != nil {
			return nil, err
		}
		tails, err := h.mainFloors()
		if err != nil {
			return nil, err
		}
		sources = append(sources, tails...)
	}
	source := debtimport.FloorSource{Kind: "reserved", Ref: "--reserved"}
	if reserved == "" {
		source.Kind = "reserved_unspecified"
	} else if reserved != "none" {
		for _, id := range strings.Split(reserved, ",") {
			if !historyID.MatchString(id) {
				return nil, fmt.Errorf("--reserved expects comma-separated TD ids or none")
			}
			if tdNumber(id) > tdNumber(source.DebtID) {
				source.DebtID = id
			}
		}
	}
	return append(sources, source), nil
}
