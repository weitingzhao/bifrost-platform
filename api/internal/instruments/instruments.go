// Package instruments reads, from the source at main, the laws, the Skills and
// the MCP tools the system actually has, so the Owner can audit them on Ops
// (GET /api/v1/governance/instruments). Nothing here is a kept list
// (OWNER_LAW #30): every name comes from the files of the two repositories.
//
// A source that cannot be read is empty and named in Unread, never an older
// value, and does not stop the others. A single malformed item is named there
// too.
package instruments

import (
	"context"
	"errors"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	infraRepo    = "bifrost-trade-infra"
	platformRepo = "bifrost-platform"

	lawsPath      = "agent-config/OWNER_LAW.md"
	skillsDir     = "agent-config/claude/skills"
	mcpConfigPath = "agent-config/.mcp.json"
	mcpSourceDir  = "mcp/platform/src"

	keepFor  = 60 * time.Second
	readTime = 60 * time.Second
)

// Source reads the repositories. Files maps the path of every file of a commit
// to its blob id.
type Source interface {
	MainHead(ctx context.Context, repo string) (string, error)
	Files(ctx context.Context, repo, commit string) (map[string]string, error)
	Blob(ctx context.Context, repo, id string) ([]byte, error)
}

// Law is one row of the register in OWNER_LAW.md.
type Law struct {
	Number          string   `json:"number"`
	Date            string   `json:"date"`
	Text            string   `json:"text"`
	Where           string   `json:"where"`
	Enforcement     string   `json:"enforcement"`
	Status          string   `json:"status"`
	State           string   `json:"state"` // landed, open, not_applicable, unknown
	MachineEnforced bool     `json:"machine_enforced"`
	Skills          []string `json:"skills"`
}

// Skill is one agent-config/claude/skills/<name>/SKILL.md.
type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Lines       int      `json:"lines"`
	Laws        []string `json:"laws"`
}

// MCPServer is a server of .mcp.json: its name and how it is reached, and
// deliberately nothing else of its entry.
type MCPServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
}

// MCPTool is a tool of the platform MCP server.
type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// MCP is what the MCP configuration and the platform server's source declare.
type MCP struct {
	Servers []MCPServer `json:"servers"`
	Tools   []MCPTool   `json:"tools"`
}

// Commits are the two commits the answer was read from; "" when a head could
// not be read.
type Commits struct {
	Infra    string `json:"infra"`
	Platform string `json:"platform"`
}

// Unread names a source or an item the answer does not have.
type Unread struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// Report is the answer of the route.
type Report struct {
	AsOf    string   `json:"as_of"`
	Commits Commits  `json:"commits"`
	Laws    []Law    `json:"laws"`
	Skills  []Skill  `json:"skills"`
	MCP     MCP      `json:"mcp"`
	Unread  []Unread `json:"unread"`
}

// Service keeps a complete answer for a short while. An answer in which a
// source could not be read is not kept: the next request reads again.
type Service struct {
	src  Source
	now  func() time.Time
	gate chan struct{}
	kept *Report
	at   time.Time
}

// New returns a Service reading from src.
func New(src Source) *Service {
	return &Service{src: src, now: time.Now, gate: make(chan struct{}, 1)}
}

// Read answers from the kept report or from the source; one reader at a time,
// so that a page opened twice reads Gitea once. The only error is ctx ending
// while waiting for that reader.
func (s *Service) Read(ctx context.Context) (Report, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return Report{}, ctx.Err()
	}
	now := s.now()
	if s.kept != nil && now.Sub(s.at) < keepFor {
		return *s.kept, nil
	}
	s.kept = nil
	// The read outlives a caller who leaves: the next one is waiting for it.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTime)
	defer cancel()
	report, complete := build(readCtx, s.src, now)
	if complete {
		s.kept, s.at = &report, now
	}
	return report, nil
}

// reading is one build of the report.
type reading struct {
	ctx      context.Context
	src      Source
	unread   []Unread
	complete bool
}

// failed records a source that could not be read.
func (r *reading) failed(source string, err error) {
	r.complete = false
	r.unread = append(r.unread, Unread{Source: source, Error: publicError(err)})
}

// malformed records an item that was read and is not what it should be. It
// stays wrong until the file changes, so the answer may still be kept.
func (r *reading) malformed(source, problem string) {
	r.unread = append(r.unread, Unread{Source: source, Error: problem})
}

// publicError is what a viewer may read of a failed read: not the address it
// went to, nor what the credential lookup said.
func publicError(err error) string {
	var addr *url.Error
	if errors.As(err, &addr) {
		return "gitea could not be reached: " + addr.Err.Error()
	}
	if strings.HasPrefix(err.Error(), "gitea access") {
		return "gitea access is not available"
	}
	return err.Error()
}

// repoFiles is one repository at its main.
type repoFiles struct {
	name   string
	commit string
	files  map[string]string
	err    error
}

func (r *reading) open(repo string) repoFiles {
	out := repoFiles{name: repo}
	out.commit, out.err = r.src.MainHead(r.ctx, repo)
	if out.err == nil {
		out.files, out.err = r.src.Files(r.ctx, repo, out.commit)
	}
	return out
}

func sourceName(repo, file string) string { return repo + ":" + file }

// file reads one file of the repository; a failure is recorded against it.
func (r *reading) file(repo repoFiles, file string) ([]byte, bool) {
	source := sourceName(repo.name, file)
	if repo.err != nil {
		r.failed(source, repo.err)
		return nil, false
	}
	id, ok := repo.files[file]
	if !ok {
		r.failed(source, errors.New("no such file at main"))
		return nil, false
	}
	raw, err := r.src.Blob(r.ctx, repo.name, id)
	if err != nil {
		r.failed(source, err)
		return nil, false
	}
	return raw, true
}

func build(ctx context.Context, src Source, now time.Time) (Report, bool) {
	r := &reading{ctx: ctx, src: src, complete: true}
	infra, platform := r.open(infraRepo), r.open(platformRepo)
	out := Report{AsOf: now.UTC().Format(time.RFC3339), Laws: []Law{}, Skills: []Skill{},
		MCP: MCP{Servers: []MCPServer{}, Tools: []MCPTool{}}}
	if infra.err == nil {
		out.Commits.Infra = infra.commit
	}
	if platform.err == nil {
		out.Commits.Platform = platform.commit
	}
	if raw, ok := r.file(infra, lawsPath); ok {
		out.Laws = parseLaws(r, string(raw))
	}
	out.Skills = readSkills(r, infra)
	linkLawsAndSkills(r, out.Laws, out.Skills)
	if raw, ok := r.file(infra, mcpConfigPath); ok {
		out.MCP.Servers = parseServers(r, raw)
	}
	out.MCP.Tools = readTools(r, platform)
	out.Unread = r.unread
	if out.Unread == nil {
		out.Unread = []Unread{}
	}
	return out, r.complete
}

// childFiles are the paths of files directly under dir, sorted.
func childFiles(files map[string]string, dir string) []string {
	var out []string
	for p := range files {
		if path.Dir(p) == dir {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
