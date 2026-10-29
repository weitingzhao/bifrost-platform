package server

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

type commitTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Read the independently generated, paged tree, pinned by commit SHA.
// Gitea 1.21 sets truncated even on the final nonempty page when the total
// exceeds per_page. Completion therefore requires total_count distinct entries;
// a short/empty page, changing total/root, or missing pagination fields fails.
func commitTree(get func(string, int64) ([]byte, error), endpoint string) (map[string]commitTreeEntry, error) {
	entries := map[string]commitTreeEntry{}
	total, root := -1, ""
	for page := 1; page <= 1000; page++ {
		body, err := get(fmt.Sprintf("%s?recursive=true&per_page=1000&page=%d", endpoint, page), maxCommitDiff)
		if err != nil {
			return nil, err
		}
		var doc struct {
			SHA       string            `json:"sha"`
			Page      *int              `json:"page"`
			Total     *int              `json:"total_count"`
			Truncated *bool             `json:"truncated"`
			Tree      []commitTreeEntry `json:"tree"`
		}
		if err := json.Unmarshal(body, &doc); err != nil || doc.Page == nil || *doc.Page != page || doc.Total == nil ||
			*doc.Total < 0 || *doc.Total > 100000 || doc.Truncated == nil || !commitID.MatchString(doc.SHA) {
			return nil, fmt.Errorf("gitea: invalid tree page %d", page)
		}
		if page == 1 {
			total, root = *doc.Total, doc.SHA
		}
		if total != *doc.Total || root != doc.SHA {
			return nil, fmt.Errorf("gitea: tree changed across pages")
		}
		for _, e := range doc.Tree {
			if e.Path == "" || e.Path == "." || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "/") || e.Path == ".." || strings.HasPrefix(e.Path, "../") ||
				strings.ContainsAny(e.Path, "\x00\r\n\t") || !commitID.MatchString(e.SHA) ||
				!((e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755" || e.Mode == "120000")) ||
					(e.Type == "tree" && e.Mode == "040000") || (e.Type == "commit" && e.Mode == "160000")) {
				return nil, fmt.Errorf("gitea: invalid tree entry")
			}
			if _, exists := entries[e.Path]; exists {
				return nil, fmt.Errorf("gitea: duplicate tree entry")
			}
			entries[e.Path] = e
		}
		if len(entries) > total {
			return nil, fmt.Errorf("gitea: tree exceeds total_count")
		}
		if len(entries) == total {
			return entries, nil
		}
		if len(doc.Tree) == 0 || !*doc.Truncated {
			return nil, fmt.Errorf("gitea: incomplete tree")
		}
	}
	return nil, fmt.Errorf("gitea: tree page limit exceeded")
}

func completeDiffPaths(before, after map[string]commitTreeEntry, paths []string) error {
	changed := map[string]bool{}
	for p, a := range before {
		if a.Type != "tree" && a != after[p] {
			changed[p] = true
		}
	}
	for p, b := range after {
		if b.Type != "tree" && b != before[p] {
			changed[p] = true
		}
	}
	for _, p := range paths {
		// Unchanged source endpoints of a copy may also occur in the diff.
		if _, a := before[p]; !a {
			if _, b := after[p]; !b {
				return fmt.Errorf("gitea: diff names a path absent from both trees")
			}
		}
		delete(changed, p)
	}
	if len(changed) != 0 {
		return fmt.Errorf("gitea: diff omits %d changed tree paths", len(changed))
	}
	return nil
}
