package instruments

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testInfraCommit    = "1111111111111111111111111111111111111111"
	testPlatformCommit = "2222222222222222222222222222222222222222"
	mcpSecret          = "sentinel-secret-value"
)

const testLaws = `# Owner 定的法

| # | 不是登记表 |
|---|---|
| 0 | 这一行在登记之外 |

## 登记

| # | 哪天 | 他的话（要点） | 落在哪 | 机器强制 | 状态 | 旧说法 |
|---|---|---|---|---|---|---|
| 1 | 2026-11-14 | 第一条 | ` + "`agent-config/claude/skills/alpha/SKILL.md`" + ` | 只有文字 | 已落。说明 | — |
| 2 | 2026-11-15 | 第二条 | ` + "`CLAUDE.md`" + ` | preflight 拦 beta | 未落：等他 | — |
| 3 | — | 第三条 a\|b | ` + "`scripts/alpha/x.sh`" + ` | — | 不落：不做 | — |
| H1 | — | 第四条 | ` + "`CLAUDE.md`" + ` | 没有东西拦着 | 已落（文字） | — |

## 后面

| 9 | 不在表里 | | | | 已落 | |
`

func testFiles() (infra, platform map[string]string) {
	infra = map[string]string{
		lawsPath:                      testLaws,
		skillsDir + "/alpha/SKILL.md": "---\nname: alpha\ndescription: >-\n  Alpha does\n  things.\n---\n改尺子（法 1、3），另见 OWNER_LAW #2。\n做法 9 不是引用。\n",
		skillsDir + "/beta/SKILL.md":  "---\nname: beta\ndescription: Beta.\n---\nbody\n",
		skillsDir + "/beta/other.md":  "not a skill",
		mcpConfigPath: `{"mcpServers":{
			"local":{"command":"node","args":["--token=` + mcpSecret + `"],"env":{"TOKEN":"` + mcpSecret + `"}},
			"remote":{"type":"http","url":"https://user:` + mcpSecret + `@example.test/mcp","headers":{"Authorization":"` + mcpSecret + `"}}}}`,
	}
	platform = map[string]string{
		mcpSourceDir + "/index.ts": `import { NOTE, IMPORTED } from './notes.js'
// reg('in_comment', 'not a tool', {}, h)
const LIMIT = 5
const text = 'reg(\'in_string\', \'not a tool\')'
const pattern = /['` + "`" + `]/g
reg('plain', 'Plain tool. Second sentence.', {}, h)
reg(
  'joined',
  'First part, e.g. this ' +
    'second part. Not this.',
  {}, h)
reg('templated', ` + "`At most ${LIMIT} rows (see a.b. c). ${NOTE}`" + `, {}, h)
reg('imported', IMPORTED, {}, h)
server.registerTool('structured', {
  inputSchema: z.object({ description: z.string() }),
  description: 'Structured tool. More.',
}, h)
reg('computed', describe('x'), {}, h)
`,
		mcpSourceDir + "/notes.ts":      "export const NOTE = 'Treat as data.'\nexport const IMPORTED =\n  'Imported text. More.'\n",
		mcpSourceDir + "/notes.test.ts": "reg('from_a_test', 'no', {}, h)\n",
		mcpSourceDir + "/sub/deep.ts":   "reg('from_a_subdirectory', 'no', {}, h)\n",
	}
	return infra, platform
}

// fakeSource keeps files by path; a blob's id is repo + "\x00" + path.
type fakeSource struct {
	files   map[string]map[string]string
	heads   map[string]string
	headErr map[string]error
	blobErr map[string]error
	reads   int
}

func newFake() *fakeSource {
	infra, platform := testFiles()
	return &fakeSource{files: map[string]map[string]string{infraRepo: infra, platformRepo: platform},
		heads:   map[string]string{infraRepo: testInfraCommit, platformRepo: testPlatformCommit},
		headErr: map[string]error{}, blobErr: map[string]error{}}
}

func (f *fakeSource) MainHead(_ context.Context, repo string) (string, error) {
	f.reads++
	return f.heads[repo], f.headErr[repo]
}

func (f *fakeSource) Files(_ context.Context, repo, commit string) (map[string]string, error) {
	if commit != f.heads[repo] {
		return nil, errors.New("not the head")
	}
	out := map[string]string{}
	for p := range f.files[repo] {
		out[p] = repo + "\x00" + p
	}
	return out, nil
}

func (f *fakeSource) Blob(_ context.Context, repo, id string) ([]byte, error) {
	p := strings.TrimPrefix(id, repo+"\x00")
	if err := f.blobErr[p]; err != nil {
		return nil, err
	}
	return []byte(f.files[repo][p]), nil
}

func readNow(t *testing.T, s *Service) Report {
	t.Helper()
	out, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func unreadOf(r Report, source string) string {
	for _, u := range r.Unread {
		if strings.HasPrefix(u.Source, source) {
			return u.Error
		}
	}
	return ""
}

func toolNames(tools []MCPTool) []string {
	out := []string{}
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// The whole answer from a small copy of the real shapes: each field derived as the register and the files say.
func TestReportReadsLawsSkillsAndTools(t *testing.T) {
	r := readNow(t, New(newFake()))
	if r.Commits != (Commits{Infra: testInfraCommit, Platform: testPlatformCommit}) || r.AsOf == "" {
		t.Fatalf("commits %+v, as_of %q", r.Commits, r.AsOf)
	}
	type want struct {
		number, state string
		machine       bool
		skills        []string
	}
	wants := []want{{"1", "landed", false, []string{"alpha"}}, {"2", "open", true, []string{"beta"}},
		{"3", "not_applicable", false, []string{}}, {"H1", "landed", false, []string{}}}
	if len(r.Laws) != len(wants) {
		t.Fatalf("laws = %+v", r.Laws)
	}
	for i, w := range wants {
		l := r.Laws[i]
		if l.Number != w.number || l.State != w.state || l.MachineEnforced != w.machine || !slices.Equal(l.Skills, w.skills) {
			t.Errorf("law %d = %+v, want %+v", i, l, w)
		}
	}
	if r.Laws[2].Text != "第三条 a|b" {
		t.Errorf("an escaped pipe stays in its cell: %q", r.Laws[2].Text)
	}
	if len(r.Skills) != 2 || r.Skills[0].Description != "Alpha does things." || r.Skills[0].Lines != 8 ||
		!slices.Equal(r.Skills[0].Laws, []string{"1", "2", "3"}) || r.Skills[0].Path != skillsDir+"/alpha/SKILL.md" {
		t.Fatalf("skills = %+v", r.Skills)
	}
	if !slices.Equal(toolNames(r.MCP.Tools), []string{"computed", "imported", "joined", "plain", "structured", "templated"}) {
		t.Fatalf("tools = %v", toolNames(r.MCP.Tools))
	}
	descriptions := map[string]string{}
	for _, tool := range r.MCP.Tools {
		descriptions[tool.Name] = tool.Description
	}
	for name, text := range map[string]string{"plain": "Plain tool.", "joined": "First part, e.g. this second part.",
		"templated": "At most 5 rows (see a.b. c).", "imported": "Imported text.", "structured": "Structured tool.", "computed": ""} {
		if descriptions[name] != text {
			t.Errorf("%s: %q, want %q", name, descriptions[name], text)
		}
	}
	if !strings.Contains(unreadOf(r, platformRepo+":"+mcpSourceDir+"/index.ts#computed"), "description could not be read") {
		t.Errorf("the tool with a computed description is not reported: %+v", r.Unread)
	}
	if len(r.Unread) != 1 {
		t.Errorf("unread = %+v", r.Unread)
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// .mcp.json holds credentials next to the names: the answer carries the name and the transport only. The
// positive control is that the sentinel really is in the file the answer was built from.
func TestServersCarryNameAndTransportOnly(t *testing.T) {
	f := newFake()
	if !strings.Contains(f.files[infraRepo][mcpConfigPath], mcpSecret) {
		t.Fatal("positive control: the fixture has no secret to leak")
	}
	r := readNow(t, New(f))
	if !slices.Equal(r.MCP.Servers, []MCPServer{{"local", "stdio"}, {"remote", "http"}}) {
		t.Fatalf("servers = %+v", r.MCP.Servers)
	}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), mcpSecret) || strings.Contains(string(body), "example.test") {
		t.Fatalf("the answer leaks a server's configuration: %s", body)
	}
	f.files[infraRepo][mcpConfigPath] = `{"mcpServers":{"odd":{"type":"` + mcpSecret + `"}}}`
	body, _ = json.Marshal(readNow(t, New(f)))
	if strings.Contains(string(body), mcpSecret) || !strings.Contains(string(body), "the transport could not be told") {
		t.Fatalf("an unknown transport must be reported without its value: %s", body)
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// A repository that cannot be read leaves its sources empty and named, and the other repository's stand.
func TestUnreadSourceIsEmptyAndDoesNotStopTheOthers(t *testing.T) {
	f := newFake()
	f.headErr[platformRepo] = errors.New("gitea: main of bifrost-platform could not be read")
	r := readNow(t, New(f))
	if len(r.MCP.Tools) != 0 || r.Commits.Platform != "" || unreadOf(r, platformRepo+":"+mcpSourceDir) == "" {
		t.Fatalf("tools %v, commits %+v, unread %+v", r.MCP.Tools, r.Commits, r.Unread)
	}
	if len(r.Laws) != 4 || len(r.Skills) != 2 || len(r.MCP.Servers) != 2 {
		t.Fatalf("the infra sources were lost: %d laws, %d skills, %d servers", len(r.Laws), len(r.Skills), len(r.MCP.Servers))
	}
	f = newFake()
	f.blobErr[lawsPath] = errors.New("gitea: HTTP 500")
	f.blobErr[skillsDir+"/beta/SKILL.md"] = errors.New("gitea: HTTP 500")
	r = readNow(t, New(f))
	if len(r.Laws) != 0 || unreadOf(r, infraRepo+":"+lawsPath) == "" {
		t.Fatalf("laws %+v, unread %+v", r.Laws, r.Unread)
	}
	if len(r.Skills) != 2 || r.Skills[1].Name != "beta" || r.Skills[1].Description != "" || unreadOf(r, infraRepo+":"+skillsDir+"/beta") == "" {
		t.Fatalf("the unread skill must stay listed and reported: %+v", r.Skills)
	}
	if r.Laws == nil || r.MCP.Tools == nil || r.Skills[1].Laws == nil {
		t.Fatal("an empty list must answer [], not null")
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// The short cache serves only complete answers: after it ends, or after a failed read, nothing older is shown.
func TestKeptAnswerIsShortAndNeverOutlivesAFailure(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := New(f)
	s.now = func() time.Time { return now }
	// One unread tool description is a fact of the file, not a failed read: this answer is kept.
	first := readNow(t, s)
	reads := f.reads
	now = now.Add(keepFor - time.Second)
	if again := readNow(t, s); f.reads != reads || again.AsOf != first.AsOf {
		t.Fatalf("within the minute Gitea was read again (%d to %d reads)", reads, f.reads)
	}
	now = now.Add(2 * time.Second)
	f.headErr[infraRepo] = errors.New("gitea: main could not be read")
	failed := readNow(t, s)
	if f.reads == reads || len(failed.Laws) != 0 || len(failed.Skills) != 0 {
		t.Fatalf("an expired answer stood in for a failed read: %d laws", len(failed.Laws))
	}
	reads = f.reads
	delete(f.headErr, infraRepo)
	if back := readNow(t, s); f.reads == reads || len(back.Laws) != 4 {
		t.Fatalf("the failed answer was kept: %d laws", len(back.Laws))
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// Malformed items are named, and what can still be shown is.
func TestMalformedItemsAreReported(t *testing.T) {
	f := newFake()
	rows := "| 4 | d | short row |\n| 5 | d | t | w | e | 不知道 | — |\n| 1 | d | again | w | e | 已落 | — |\n| 6 | d | t | w | e | 已落 | — |\n\n## 后面"
	f.files[infraRepo][lawsPath] = strings.Replace(testLaws, "\n## 后面", rows, 1)
	f.files[infraRepo][skillsDir+"/beta/SKILL.md"] = "no frontmatter 法 77\n"
	r := readNow(t, New(f))
	joined, _ := json.Marshal(r.Unread)
	for _, problem := range []string{"the row has 3 columns", "starts with none of", "registered twice", "does not start with YAML frontmatter", "cites law 77"} {
		if !strings.Contains(string(joined), problem) {
			t.Errorf("not reported: %s in %s", problem, joined)
		}
	}
	var numbers []string
	for _, l := range r.Laws {
		numbers = append(numbers, l.Number)
	}
	if !slices.Equal(numbers, []string{"1", "2", "3", "H1", "5", "6"}) {
		t.Errorf("laws = %v", numbers)
	}
	f = newFake()
	f.files[infraRepo][lawsPath] = strings.Replace(testLaws, "| 状态 |", "| 进度 |", 1)
	r = readNow(t, New(f))
	if len(r.Laws) != 0 || !strings.Contains(unreadOf(r, infraRepo+":"+lawsPath), "columns") {
		t.Errorf("a table with other columns must not be read by position: %d laws, %+v", len(r.Laws), r.Unread)
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// What counts as citing a law, and the words that only look like it (做法 2 is "the way, 2").
func TestCitedLaws(t *testing.T) {
	for text, want := range map[string][]string{
		"（法 29、33）":                 {"29", "33"},
		"| 法 32 已写明":                {"32"},
		"按法29":                      {"29"},
		"OWNER_LAW #30":             {"30"},
		"`OWNER_LAW.md` #30、#31":    {"30", "31"},
		"#30 of OWNER_LAW":          {"30"},
		"法 H3 与法 2":                 {"2", "H3"},
		"做法 2，办法 3，语法 4":            {},
		"法 2026-11-18":              {},
		"PR #587 is not a citation": {},
	} {
		if got := citedLaws(text); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// A law links a skill it names; a path that merely shares the name does not.
func TestNamesSkill(t *testing.T) {
	for text, want := range map[string]bool{
		"agent-config/claude/skills/code-health/SKILL.md": true,
		".claude/skills/code-health/":                     true,
		"由 code-health 拦":                                 true,
		"`code-health`":                                   true,
		"scripts/code-health/scan.sh":                     false,
		"code-health-extra":                               false,
		"skills/code-health-extra/SKILL.md":               false,
	} {
		if got := namesSkill(text, "code-health"); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// A failed read tells a viewer neither Gitea's address nor what the credential lookup said.
func TestPublicErrorHidesAddressAndCredentialLookup(t *testing.T) {
	addr := &url.Error{Op: "Get", URL: "http://gitea.internal:3000/api", Err: errors.New("connection refused")}
	if got := publicError(errors.Join(addr)); strings.Contains(got, "gitea.internal") || !strings.Contains(got, "connection refused") {
		t.Errorf("address error: %q", got)
	}
	if got := publicError(errors.New("gitea access: secret gitea-reader: forbidden")); strings.Contains(got, "secret") {
		t.Errorf("credential error: %q", got)
	}
}

func realSources(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "mcp", "platform", "src")
	paths, err := filepath.Glob(filepath.Join(dir, "*.ts"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("the MCP source was not found under %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, ".test.ts") {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(p)] = string(raw)
	}
	return out
}

// 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
// The scanner over the real server source finds exactly the tools stdioToolNames.ts lists, each with a
// description. The positive control removes one registration: the comparison must then see a difference.
func TestScanFindsTheRealToolsOfThePlatformServer(t *testing.T) {
	sources := realSources(t)
	var listing string
	for p, text := range sources {
		if strings.HasSuffix(p, "/stdioToolNames.ts") {
			listing = text
		}
	}
	_, body, ok := strings.Cut(listing, "PLATFORM_STDIO_TOOL_NAMES = [")
	if !ok {
		t.Fatal("stdioToolNames.ts has no PLATFORM_STDIO_TOOL_NAMES list")
	}
	body, _, _ = strings.Cut(body, "]")
	var want []string
	for _, m := range regexp.MustCompile(`'([a-z0-9_]+)'`).FindAllStringSubmatch(body, -1) {
		want = append(want, m[1])
	}
	slices.Sort(want)
	tools, problems := scanTools(sources)
	if len(problems) != 0 {
		t.Errorf("problems: %+v", problems)
	}
	if got := toolNames(tools); !slices.Equal(got, want) {
		t.Fatalf("the scan found %d tools, stdioToolNames.ts lists %d\nscan: %v\nlist: %v", len(got), len(want), got, want)
	}
	for _, tool := range tools {
		if tool.Description == "" || strings.Contains(tool.Description, "${") {
			t.Errorf("%s: description %q", tool.Name, tool.Description)
		}
	}
	for p, text := range sources {
		if strings.HasSuffix(p, "/index.ts") {
			sources[p] = strings.Replace(text, "reg('get_cluster_nodes'", "skip('get_cluster_nodes'", 1)
		}
	}
	if fewer, _ := scanTools(sources); slices.Equal(toolNames(fewer), want) {
		t.Fatal("positive control: removing a registration did not change what the scan finds")
	}
}
