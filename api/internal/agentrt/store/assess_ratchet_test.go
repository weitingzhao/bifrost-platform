package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TD-391's ratchet: an assessment is advice, and no code path takes it to an
// answer or a decision. The code that lists what is assessed, asks for an
// assessment, records it and ends it calls nothing that answers, acknowledges
// or withdraws a card, or approves, rejects, resolves or claims an approval;
// the runtime's files of it import neither the question store nor the
// approvals (the server's reader may, to read them). The Owner's route only
// records an opt-in; the controller records dispatch and results. The one
// reader that acts on an assessment, the card-auto-answer rule, only holds
// its own answer back (TestAssessmentOfWorkOnMain).
func TestAssessmentsCannotAnswer(t *testing.T) {
	t.Parallel()
	acting := regexp.MustCompile(`(?i)^(answer|approve|reject|resolve|decide|ack|withdraw|claim|ask)`)
	for _, f := range []struct {
		path    string
		imports bool // may import the question store and the approvals, to read them
	}{
		{"assess.go", false},
		{"assessaccept.go", false},
		{"assessrequest.go", false},
		{filepath.Join("..", "agentrtapi", "assess.go"), false},
		{filepath.Join("..", "controller", "assess.go"), false},
		{filepath.Join("..", "..", "server", "agentrt_assess.go"), true},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), f.path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !f.imports && (strings.HasSuffix(p, "/internal/questions") || strings.HasSuffix(p, "/internal/approvals")) {
				t.Errorf("%s imports %s", f.path, p)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			var name string
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			}
			if acting.MatchString(name) {
				t.Errorf("%s calls %s", f.path, name)
			}
			return true
		})
	}
}
