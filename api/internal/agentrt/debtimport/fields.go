package debtimport

import (
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	oldEvidence  = regexp.MustCompile("(?m)^\\s+- `([^`]*)`\\s+—\\s+`(.*)`\\s*$")
	pathLine     = regexp.MustCompile("([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*-]+)+|[A-Za-z0-9_-]+(?:\\.[A-Za-z0-9_-]+)+):(\\d+)(?:[-–](\\d+))?")
	evidencePath = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*-]+)+`)
	reposTail    = regexp.MustCompile(`repos:\s*(.*)$`)
)

func approvalText(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "- 审批") {
			return l
		}
	}
	return ""
}
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
func classifyApproval(s string) (string, bool, bool) {
	if slices.Contains([]string{"none", "public_interface", "ddl", "security", "delete", "prod_change", "cluster_write", "rule_sign", "cross_repo_release", "other"}, s) {
		return s, false, true
	}
	// A directional decision gates starting. No text claiming prior approval is
	// treated as an executed approval record.
	if containsAny(s, "改公开接口", "Owner（接口）", "选法", "要你定", "选做法", "Owner 定", "要批（设计）") && !strings.HasPrefix(s, "不需要") && !strings.HasPrefix(s, "无") {
		return "none", true, true
	}
	if slices.Contains([]string{"不用批", "不需要", "无", "不要"}, s) || startsAny(s, "不用批（", "不需要（", "无（", "不要（") {
		return "none", false, true
	}
	for _, v := range []struct {
		gate  string
		words []string
	}{
		{"security", []string{"安全", "凭据", "RBAC", "准入策略", "信任锚", "口令", "钥匙"}},
		{"ddl", []string{"DDL", "改表", "迁移"}},
		{"rule_sign", []string{"签规则", "签 STG 规则", "治理文字"}},
		{"public_interface", []string{"删公开路由", "新路由", "新接口"}},
		{"delete", []string{"删"}},
		{"prod_change", []string{"PROD 变更", "PROD 发布"}},
		{"cluster_write", []string{"apply_manifest", "集群", "apply ", "流水线", "helm upgrade", "cicd"}},
		{"cross_repo_release", []string{"跨仓库发版"}},
	} {
		if containsAny(s, v.words...) {
			return v.gate, false, true
		}
	}
	return "other", false, false
}
func startsAny(s string, prefixes ...string) bool {
	for _, v := range prefixes {
		if strings.HasPrefix(s, v) {
			return true
		}
	}
	return false
}

func (p *parser) evidence(id, body string, repos []string) {
	if len(repos) == 0 {
		// A unique repository in the paragraph qualifies shortened filenames.
		candidates := []string{}
		for _, file := range evidencePath.FindAllString(body, -1) {
			repo, _ := p.resolvePath(file, nil)
			if repo != "" && !slices.Contains(candidates, repo) {
				candidates = append(candidates, repo)
			}
		}
		if len(candidates) == 1 {
			repos = candidates
		}
	}
	matches := oldEvidence.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 || strings.TrimSpace(oldEvidence.ReplaceAllString(body, "")) != "" {
		quote := body
		if utf8.RuneCountInString(body) > 400 {
			quote = ""
			p.finding(id, 6, "measurement_text", "measurement exceeds quote limit; whole body retained in debt_text(measured), measurement quote is omitted")
		}
		p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "measurement", Quote: quote})
	}
	for _, m := range matches {
		p.codeRef(id, m[1], m[2], "", repos)
	}
	// Free-form evidence adds code references without discarding its prose.
	free := oldEvidence.ReplaceAllString(body, "")
	commits := extractCommits(free)
	commit := ""
	if len(commits) == 1 {
		commit = commits[0]
	} else if len(commits) > 1 {
		p.finding(id, 6, "ambiguous_evidence_commit", strings.Join(commits, ", "))
	}
	for _, m := range pathLine.FindAllStringSubmatchIndex(free, -1) {
		if !fileLike(free[m[2]:m[3]]) {
			continue
		}
		location := free[m[0]:m[1]]
		if workspaceSlash(free, m[0]) {
			location = "/" + location
		}
		p.codeRef(id, location, "", commit, repos)
	}
	for _, sha := range commits {
		p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "commit", CommitSHA: sha})
	}
}

func extractCommits(s string) []string {
	out := []string{}
	for _, sha := range shortSHA.FindAllString(s, -1) {
		// All-digit dates, counters and measurements are not commit evidence.
		if strings.ContainsAny(sha, "abcdef") && !slices.Contains(out, sha) {
			out = append(out, sha)
		}
	}
	return out
}

func (p *parser) knownRepos() []string {
	out := []string{}
	for _, proj := range p.opts.Projects {
		for _, own := range proj.Owns {
			r, _, _ := strings.Cut(own, "/")
			if r != "" && !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	for r := range p.opts.RepoRoots {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
func (p *parser) resolvePath(location string, repos []string) (string, string) {
	for _, r := range p.knownRepos() {
		if strings.HasPrefix(location, r+"/") {
			return r, strings.TrimPrefix(location, r+"/")
		}
	}
	candidates := []string{}
	for r, prefixes := range p.opts.RepoRoots {
		for _, prefix := range prefixes {
			if strings.HasPrefix(location, prefix) && !slices.Contains(candidates, r) {
				candidates = append(candidates, r)
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], location
	}
	if len(repos) == 1 {
		return repos[0], location
	}
	return "", location
}

// resolveWorkspace maps "/<workspace>/<repo>/<path>" to the earliest known
// repository segment.
func (p *parser) resolveWorkspace(location string) (string, string) {
	repo, at := "", -1
	for _, r := range p.knownRepos() {
		if i := strings.Index(location, "/"+r+"/"); i >= 0 && (at < 0 || i < at) {
			repo, at = r, i
		}
	}
	if repo == "" {
		return "", location
	}
	return repo, location[at+len(repo)+2:]
}

// workspaceSlash reports a "/" directly before a match that starts a token,
// as in "/stocks/x.md:3"; "://" and "~/" prefixes are not workspace paths.
func workspaceSlash(s string, at int) bool {
	if at == 0 || s[at-1] != '/' {
		return false
	}
	if at == 1 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:at-1])
	return unicode.IsSpace(r) || strings.ContainsRune("`'\"(（", r)
}

func (p *parser) codeRef(id, location, quote, commit string, repos []string) {
	file := location
	if !fileLike(strings.Split(location, ":")[0]) {
		p.finding(id, 6, "non_file_evidence", location)
		p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "measurement", Quote: location + " — " + quote})
		return
	}
	// A leading slash names a workspace path: its line is parsed like any other
	// location, and it never falls back to the debt's own repository.
	absolute := strings.HasPrefix(location, "/")
	relative := strings.TrimLeft(location, "/")
	from, to := 0, 0
	if m := pathLine.FindStringSubmatch(relative); m != nil && m[0] == relative {
		file = location[:len(location)-len(relative)] + m[1]
		from, _ = strconv.Atoi(m[2])
		to = from
		if m[3] != "" {
			to, _ = strconv.Atoi(m[3])
		}
	}
	repo := ""
	if absolute {
		repo, file = p.resolveWorkspace(file)
		if repo == "" {
			p.finding(id, 6, "workspace_path_outside_repos", location+"; absolute workspace path names no known repository; kept as doc_ref")
			p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "doc_ref", Path: file, LineFrom: from, LineTo: to, Quote: quote, CommitSHA: commit})
			return
		}
	} else {
		repo, file = p.resolvePath(file, repos)
	}
	if repo == "" {
		p.finding(id, 6, "unresolved_code_repo", location)
		p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "doc_ref", Path: file, LineFrom: from, LineTo: to, Quote: quote, CommitSHA: commit})
		return
	}
	if utf8.RuneCountInString(quote) > 400 {
		p.finding(id, 6, "quote_too_long", location+": quote exceeds 400 characters; retained for review")
	}
	p.req.Evidence = append(p.req.Evidence, Evidence{DebtID: id, EvidenceKind: "code_ref", Repo: repo, Path: file, LineFrom: from, LineTo: to, Quote: quote, CommitSHA: commit})
}
func (p *parser) inferredRepos(id string) []string {
	out := []string{}
	for _, e := range p.req.Evidence {
		if e.DebtID == id && e.Repo != "" && !slices.Contains(out, e.Repo) {
			out = append(out, e.Repo)
		}
	}
	for _, file := range p.evidencePaths(id) {
		repo, _ := p.resolvePath(file, nil)
		if repo != "" && !slices.Contains(out, repo) {
			out = append(out, repo)
		}
	}
	return out
}

func (p *parser) evidencePaths(id string) []string {
	out := []string{}
	for _, text := range p.req.Texts {
		if text.DebtID == id && text.TextKind == "measured" {
			out = append(out, evidencePath.FindAllString(text.Body, -1)...)
		}
	}
	return out
}

// ownsMatch adds ** and brace alternatives to path.Match, without depending on
// host filesystem layout or interpreting project keys.
func ownsMatch(pattern, name string) bool {
	if a := strings.IndexByte(pattern, '{'); a >= 0 {
		if b := strings.IndexByte(pattern[a:], '}'); b >= 0 {
			for _, v := range strings.Split(pattern[a+1:a+b], ",") {
				if ownsMatch(pattern[:a]+v+pattern[a+b+1:], name) {
					return true
				}
			}
			return false
		}
	}
	pp, nn := strings.Split(pattern, "/"), strings.Split(name, "/")
	var match func(int, int) bool
	match = func(i, j int) bool {
		if i == len(pp) {
			return j == len(nn)
		}
		if pp[i] == "**" {
			for k := j; k <= len(nn); k++ {
				if match(i+1, k) {
					return true
				}
			}
			return false
		}
		if j == len(nn) {
			return false
		}
		ok, _ := path.Match(pp[i], nn[j])
		return ok && match(i+1, j+1)
	}
	return match(0, 0)
}
func (p *parser) project(id string, repos []string) string {
	paths := []string{}
	for _, e := range p.req.Evidence {
		if e.DebtID == id && e.Repo != "" {
			paths = append(paths, e.Repo+"/"+e.Path)
		}
	}
	for _, file := range p.evidencePaths(id) {
		repo, relative := p.resolvePath(file, nil)
		if repo != "" {
			paths = append(paths, repo+"/"+relative)
		}
	}
	for _, r := range repos {
		has := false
		for _, f := range paths {
			if strings.HasPrefix(f, r+"/") {
				has = true
				break
			}
		}
		if !has {
			paths = append(paths, r+"/")
		}
	}
	candidates := []string{}
	for _, proj := range p.opts.Projects {
		for _, own := range proj.Owns {
			for _, f := range paths {
				if ownsMatch(own, f) && !slices.Contains(candidates, proj.Key) {
					candidates = append(candidates, proj.Key)
				}
			}
		}
	}
	if len(candidates) == 0 {
		p.finding(id, 7, "unmapped_project", "owns did not identify a project; explicit review required")
		return ""
	}
	if len(candidates) > 1 {
		p.finding(id, 7, "cross_project", strings.Join(candidates, ", ")+"; first matching project is a suggestion")
	}
	return candidates[0]
}

func (p *parser) acceptance(id, raw, context string) {
	normal := strings.TrimSpace(strings.ReplaceAll(raw, "**", ""))
	result := "unknown"
	switch {
	case strings.HasPrefix(normal, "部分 PASS"):
		result = "partial"
	case strings.HasPrefix(normal, "PASS"):
		result = "pass"
	case strings.HasPrefix(normal, "FAIL"):
		result = "fail"
	case strings.Contains(normal, "部分 PASS") || (strings.Contains(normal, "PASS") && strings.Contains(normal, "FAIL")):
		result = "partial"
	case strings.Contains(normal, "PASS"):
		result = "pass"
	case strings.Contains(normal, "FAIL"):
		result = "fail"
	}
	if result == "unknown" {
		p.finding(id, 9, "unmapped_acceptance", raw)
	}
	t := p.req.Source.ImportedAt
	if date := fullDate.FindString(raw); date != "" {
		v, err := time.ParseInLocation("2006-01-02", date, p.zone)
		if err == nil {
			t = v
		} else {
			p.finding(id, 9, "invalid_acceptance_date", date)
		}
	} else {
		p.finding(id, 9, "undated_acceptance", "ran_at uses imported_at")
	}
	commits := extractCommits(context)
	at := map[string][]string{}
	if len(commits) > 0 {
		at["unassigned"] = commits
		p.finding(id, 9, "unassigned_acceptance_commits", strings.Join(commits, ", ")+"; repository association requires review")
	}
	p.req.Runs = append(p.req.Runs, AcceptanceRun{id, "imported", digest(raw), result, t, at})
}

// hostSuffixes are final DNS labels that never name a source file.
var hostSuffixes = []string{"local", "lan", "svc", "internal", "cluster", "localhost", "com", "net", "io"}

// hostLike reports a slashless dotted host name, such as a Kubernetes service
// address in host:port form.
func hostLike(s string) bool {
	if strings.Contains(s, "/") {
		return false
	}
	labels := strings.Split(strings.ToLower(s), ".")
	return len(labels) > 1 && (slices.Contains(labels, "svc") || slices.Contains(hostSuffixes, labels[len(labels)-1]))
}

// Dotted numeric addresses and host names (host:port) are never files.
func fileLike(s string) bool {
	if strings.Contains(s, "/") {
		return !strings.Contains(s, "://")
	}
	if hostLike(s) {
		return false
	}
	ext := path.Ext(s)
	return len(ext) > 1 && strings.ContainsAny(ext, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

// Rule 9 explicitly names acceptance-result lines. Keep this closed recognizer
// separate from rule 3: an arbitrary unbolded bullet is never a field.
func (p *parser) plainAcceptanceResults(id, body string, offset int) {
	for _, l := range lines(body, offset) {
		prefix := ""
		for _, candidate := range []string{"- 验收结果：", "- 验收结果:"} {
			if strings.HasPrefix(l.text, candidate) {
				prefix = candidate
			}
		}
		if prefix == "" {
			continue
		}
		value := strings.TrimLeft(l.text[len(prefix):], " \t")
		start := l.end - len(value)
		n := p.addText(id, "import_raw", "验收结果", value)
		if n == 0 {
			continue
		}
		p.bind(0, start, l.end, []string{pointer("debt_text", n-1, "body")}, "string")
		p.acceptance(id, value, value)
		p.finding(id, 9, "plain_acceptance_result", "explicit rule 9 result line; not a general unbolded field")
	}
}
