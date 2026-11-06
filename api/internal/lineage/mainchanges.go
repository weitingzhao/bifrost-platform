package lineage

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Immutable commit data is shared across passes; failures are never cached.
var mainChangeCache = struct {
	sync.Mutex
	messages map[string]giteaCommit
	diffs    map[string]string
	order    []string
}{messages: map[string]giteaCommit{}, diffs: map[string]string{}}

const mainChangeCacheLimit = 4096

func cacheChange(key string, commit *giteaCommit, diff *string) {
	mainChangeCache.Lock()
	defer mainChangeCache.Unlock()
	if _, ok := mainChangeCache.messages[key]; !ok {
		if _, ok := mainChangeCache.diffs[key]; !ok {
			mainChangeCache.order = append(mainChangeCache.order, key)
		}
	}
	if commit != nil {
		mainChangeCache.messages[key] = *commit
	}
	if diff != nil {
		mainChangeCache.diffs[key] = *diff
	}
	for len(mainChangeCache.order) > mainChangeCacheLimit {
		old := mainChangeCache.order[0]
		mainChangeCache.order = mainChangeCache.order[1:]
		delete(mainChangeCache.messages, old)
		delete(mainChangeCache.diffs, old)
	}
}

// MainChanges finds pushed results by folded lineage and, within a 20-diff
// budget, identical patches. Main is read afresh so cached data cannot hide a revert.
func MainChanges(ctx context.Context, acc Access, client *http.Client, repo string, since time.Time, maxPages int, pushed map[string][]string) (map[string]map[string]string, error) {
	g := &gitea{acc: acc, client: client}
	commits, err := g.commits(ctx, repo, "main", since, maxPages, nil)
	if err != nil {
		return nil, err
	}
	commits = standingCommits(commits)
	out := resultsForCommits(commits)
	ids := map[string]string{}
	for _, c := range commits {
		_, trailers := parseMessage(c.Msg)
		for _, id := range append(changeIDsAnywhere(c.Msg), trailers.changeID) {
			if id != "" && ids[id] == "" {
				ids[id] = c.SHA
			}
		}
	}
	prefix := acc.Base + "/" + acc.Org + "/" + repo + "/"
	budget := 20
	diff := func(sha string) (string, bool, error) {
		key := prefix + sha
		mainChangeCache.Lock()
		id, ok := mainChangeCache.diffs[key]
		mainChangeCache.Unlock()
		if ok {
			return id, true, nil
		}
		if budget == 0 {
			return "", false, nil
		}
		budget--
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, acc.Base+"/api/v1"+g.repoPath(repo)+"/git/commits/"+url.PathEscape(sha)+".diff", nil)
		if err != nil {
			return "", false, err
		}
		if acc.User != "" && acc.Pass != "" {
			req.SetBasicAuth(acc.User, acc.Pass)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", false, fmt.Errorf("commit diff %s: status %d", sha, resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		if err != nil {
			return "", false, err
		}
		if len(body) > 8<<20 {
			return "", false, fmt.Errorf("commit diff %s too large", sha)
		}
		id = verbatimPatchID(string(body))
		cacheChange(key, nil, &id)
		return id, true, nil
	}
	for task, shas := range pushed {
		for _, sha := range shas {
			if out[task][sha] != "" {
				continue
			}
			key := prefix + sha
			mainChangeCache.Lock()
			c, ok := mainChangeCache.messages[key]
			mainChangeCache.Unlock()
			if !ok {
				var raw struct {
					Message   string `json:"message"`
					Committer struct {
						Date time.Time `json:"date"`
					} `json:"committer"`
				}
				if err := g.get(ctx, g.repoPath(repo)+"/git/commits/"+url.PathEscape(sha), nil, &raw); err != nil {
					return out, err
				}
				c = giteaCommit{SHA: sha, Msg: raw.Message, At: raw.Committer.Date}
				cacheChange(key, &c, nil)
			}
			landed := ""
			_, trailers := parseMessage(c.Msg)
			for _, id := range append(changeIDsAnywhere(c.Msg), trailers.changeID) {
				if ids[id] != "" {
					landed = ids[id]
					break
				}
			}
			if landed == "" {
				var candidates []giteaCommit
				for _, main := range commits {
					_, t := parseMessage(main.Msg)
					if !main.At.Before(c.At) && len(tasksAnywhere(main.Msg)) == 0 && len(changeIDsAnywhere(main.Msg)) == 0 && t.changeID == "" {
						candidates = append(candidates, main)
					}
				}
				if len(candidates) > 0 {
					id, available, err := diff(sha)
					if err != nil {
						return out, err
					}
					if available && id != "" {
						for _, main := range candidates {
							other, _, err := diff(main.SHA)
							if err != nil {
								return out, err
							}
							if other == id {
								landed = main.SHA
								break
							}
						}
					}
				}
			}
			if landed != "" {
				if out[task] == nil {
					out[task] = map[string]string{}
				}
				out[task][sha] = landed
			}
		}
	}
	return out, nil
}

// verbatimPatchID retains whitespace, but drops index lines and hunk positions,
// as git patch-id --verbatim does. Empty diffs cannot establish a landing.
func verbatimPatchID(diff string) string {
	h := sha1.New()
	var sum [sha1.Size]byte
	started := false
	flush := func() {
		digest := h.Sum(nil)
		carry := 0
		for i := range sum {
			carry += int(sum[i]) + int(digest[i])
			sum[i] = byte(carry)
			carry >>= 8
		}
		h.Reset()
	}
	for _, line := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			if started {
				flush()
			}
			started = true
		}
		if !started || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "@@ ") {
			continue
		}
		_, _ = io.WriteString(h, line)
	}
	if !started {
		return ""
	}
	flush()
	return fmt.Sprintf("%x", sum)
}
