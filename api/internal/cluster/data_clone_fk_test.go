package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fkGraph is a small generic schema:
//
//	orders ← order_lines ← line_notes        (two levels below orders)
//	orders ← refunds                          (second direct dependent)
//	orders ← orders (parent_order_id)         (self-reference)
//	order_lines → products                    (order_lines references products)
//	orders ← audit.order_events               (dependent outside public)
var fkGraph = []dataCloneFKEdge{
	{Child: "public.order_lines", Parent: "public.orders", Constraint: "order_lines_order_fk"},
	{Child: "public.line_notes", Parent: "public.order_lines", Constraint: "line_notes_line_fk"},
	{Child: "public.refunds", Parent: "public.orders", Constraint: "refunds_order_fk"},
	{Child: "public.orders", Parent: "public.orders", Constraint: "orders_parent_fk"},
	{Child: "public.order_lines", Parent: "public.products", Constraint: "order_lines_product_fk"},
}

func childNames(edges []dataCloneFKEdge) []string {
	out := []string{}
	for _, e := range edges {
		out = append(out, e.Child)
	}
	return out
}

func TestDataCloneUnselectedDependents(t *testing.T) {
	withAudit := append(append([]dataCloneFKEdge{}, fkGraph...),
		dataCloneFKEdge{Child: "audit.order_events", Parent: "public.orders", Constraint: "order_events_order_fk"})
	cases := []struct {
		name     string
		selected []string
		edges    []dataCloneFKEdge
		want     []string
	}{
		{"parent alone pulls in direct and transitive dependents",
			[]string{"public.orders"}, fkGraph,
			[]string{"public.line_notes", "public.order_lines", "public.refunds"}},
		{"selected child still has its own unselected dependent",
			[]string{"public.orders", "public.order_lines", "public.refunds"}, fkGraph,
			[]string{"public.line_notes"}},
		{"complete closure is allowed",
			[]string{"public.orders", "public.order_lines", "public.refunds", "public.line_notes"}, fkGraph,
			[]string{}},
		{"a leaf table has no dependents",
			[]string{"public.line_notes"}, fkGraph,
			[]string{}},
		{"referencing a parent outside the selection is not a dependent",
			[]string{"public.order_lines", "public.line_notes"}, fkGraph,
			[]string{}},
		{"selecting a referenced lookup table pulls in its referrers",
			[]string{"public.products"}, fkGraph,
			[]string{"public.line_notes", "public.order_lines"}},
		{"self-reference alone is fine",
			[]string{"public.orders"}, []dataCloneFKEdge{fkGraph[3]},
			[]string{}},
		{"dependent outside public is reported",
			[]string{"public.orders", "public.order_lines", "public.refunds", "public.line_notes"}, withAudit,
			[]string{"audit.order_events"}},
		{"no foreign keys",
			[]string{"public.orders"}, nil,
			[]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := childNames(dataCloneUnselectedDependents(tc.selected, tc.edges))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestDataCloneUnselectedDependentsNamesTheEdgeThatReachedIt(t *testing.T) {
	missing := dataCloneUnselectedDependents([]string{"public.orders"}, fkGraph)
	byChild := map[string]dataCloneFKEdge{}
	for _, m := range missing {
		byChild[m.Child] = m
	}
	if e := byChild["public.line_notes"]; e.Parent != "public.order_lines" || e.Constraint != "line_notes_line_fk" {
		t.Fatalf("line_notes should be reached through order_lines, got %+v", e)
	}
	if e := byChild["public.order_lines"]; e.Parent != "public.orders" {
		t.Fatalf("order_lines should be reached from orders, got %+v", e)
	}
}

func TestDataCloneUnselectedDependentsHandlesCycles(t *testing.T) {
	cycle := []dataCloneFKEdge{
		{Child: "public.a", Parent: "public.b", Constraint: "a_b"},
		{Child: "public.b", Parent: "public.a", Constraint: "b_a"},
		{Child: "public.c", Parent: "public.b", Constraint: "c_b"},
	}
	got := childNames(dataCloneUnselectedDependents([]string{"public.a"}, cycle))
	if !reflect.DeepEqual(got, []string{"public.b", "public.c"}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseDataCloneFKEdges(t *testing.T) {
	edges, err := parseDataCloneFKEdges("public.order_lines\tpublic.orders\torder_lines_order_fk\n\npublic.refunds\tpublic.orders\trefunds_order_fk\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []dataCloneFKEdge{
		{Child: "public.order_lines", Parent: "public.orders", Constraint: "order_lines_order_fk"},
		{Child: "public.refunds", Parent: "public.orders", Constraint: "refunds_order_fk"},
	}
	if !reflect.DeepEqual(edges, want) {
		t.Fatalf("got %+v", edges)
	}
	if _, err := parseDataCloneFKEdges("public.a|public.b|a_b\n"); err == nil {
		t.Fatal("expected an error for a row that is not tab separated")
	}
}

func TestErrCloneFKClosureMessage(t *testing.T) {
	err := &ErrCloneFKClosure{Target: "bifrost_dev", Missing: dataCloneUnselectedDependents([]string{"public.orders"}, fkGraph)}
	msg := err.Error()
	for _, want := range []string{
		"selective clone refused: in bifrost_dev",
		"3 table(s)",
		"public.line_notes (references public.order_lines via line_notes_line_fk)",
		"add them to tables[]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if got := err.MissingTables(); !reflect.DeepEqual(got, []string{"line_notes", "order_lines", "refunds"}) {
		t.Fatalf("missing tables %v", got)
	}

	outside := &ErrCloneFKClosure{Target: "bifrost_stg", Missing: []dataCloneFKEdge{
		{Child: "audit.order_events", Parent: "public.orders", Constraint: "order_events_order_fk"},
	}}
	if !strings.Contains(outside.Error(), "use mode=full") || strings.Contains(outside.Error(), "add them to tables[]") {
		t.Fatalf("dependent outside public should point at mode=full: %q", outside.Error())
	}
	if got := outside.MissingTables(); !reflect.DeepEqual(got, []string{"audit.order_events"}) {
		t.Fatalf("missing tables %v", got)
	}
}

// fkEdgesExec answers the FK catalog query per target and records every command.
func fkEdgesExec(perTarget map[string]string, seen *[]string) func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
	return func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		joined := strings.Join(command, " ")
		*seen = append(*seen, joined)
		if strings.Contains(joined, dataCloneFKEdgesSQL) {
			for i, arg := range command {
				if arg == "-d" && i+1 < len(command) {
					return perTarget[command[i+1]], nil
				}
			}
		}
		return "", nil
	}
}

const ordersEdgesOut = "public.line_notes\tpublic.order_lines\tline_notes_line_fk\n" +
	"public.order_lines\tpublic.orders\torder_lines_order_fk\n" +
	"public.refunds\tpublic.orders\trefunds_order_fk\n"

func TestCheckSelectiveFKClosureChecksEveryTarget(t *testing.T) {
	var seen []string
	svc := NewService(nil)
	svc.SetPodExecForTest(fkEdgesExec(map[string]string{
		"bifrost_dev": "public.refunds\tpublic.orders\trefunds_order_fk\n",
		"bifrost_stg": ordersEdgesOut,
	}, &seen))

	err := svc.checkSelectiveFKClosure(context.Background(), "pod", []string{"bifrost_dev", "bifrost_stg"}, []string{"orders", "refunds"})
	var refused *ErrCloneFKClosure
	if !errors.As(err, &refused) {
		t.Fatalf("expected ErrCloneFKClosure, got %v", err)
	}
	if refused.Target != "bifrost_stg" || !reflect.DeepEqual(refused.MissingTables(), []string{"line_notes", "order_lines"}) {
		t.Fatalf("refused %+v", refused)
	}
	for _, s := range seen {
		if strings.Contains(s, "TRUNCATE") || strings.Contains(s, "pg_dump") {
			t.Fatalf("the check must only read the catalog, seen %q", s)
		}
	}

	err = svc.checkSelectiveFKClosure(context.Background(), "pod", []string{"bifrost_dev", "bifrost_stg"},
		[]string{"orders", "refunds", "order_lines", "line_notes"})
	if err != nil {
		t.Fatalf("complete closure should pass: %v", err)
	}
}

func TestStartDataCloneRefusesIncompleteSelectionBeforeQueueing(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", t.TempDir())
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(t.TempDir(), "last.json"))
	var seen []string
	svc := NewService(nil)
	svc.primaryOverride = "bifrost-postgres-1"
	svc.SetPodExecForTest(fkEdgesExec(map[string]string{"bifrost_dev": ordersEdgesOut}, &seen))
	svc.cloneJobs = NewDataCloneJobStore()
	svc.cloneLast = NewDataCloneLastStore()

	_, err := svc.startDataClone(context.Background(), DataCloneRequest{
		Source:            "bifrost_prod",
		Targets:           []string{"bifrost_dev"},
		Mode:              "selective",
		Tables:            []string{"orders"},
		ConfirmationToken: dataCloneConfirmTok,
		Confirm:           true,
	}, "manual", "tester")
	var refused *ErrCloneFKClosure
	if !errors.As(err, &refused) {
		t.Fatalf("expected ErrCloneFKClosure, got %v", err)
	}
	if jobs := svc.cloneJobs.List(); len(jobs) != 0 {
		t.Fatalf("a refused clone must not queue a job, got %+v", jobs)
	}
}

func TestHandleDataCloneReturnsMissingTables(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", t.TempDir())
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(t.TempDir(), "last.json"))
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULE", filepath.Join(t.TempDir(), "sched.json"))
	var seen []string
	svc := NewService(nil)
	svc.primaryOverride = "bifrost-postgres-1"
	svc.SetPodExecForTest(fkEdgesExec(map[string]string{"bifrost_dev": ordersEdgesOut}, &seen))
	h := &Handler{svc: svc}

	body := `{"source":"bifrost_prod","targets":["bifrost_dev"],"mode":"selective","tables":["orders"],` +
		`"confirmation_token":"` + dataCloneConfirmTok + `","confirm":true}`
	rec := httptest.NewRecorder()
	h.HandleDataClone(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/data-clone", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error         string            `json:"error"`
		Target        string            `json:"target"`
		MissingTables []string          `json:"missing_tables"`
		References    []dataCloneFKEdge `json:"references"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Target != "bifrost_dev" || !reflect.DeepEqual(resp.MissingTables, []string{"line_notes", "order_lines", "refunds"}) {
		t.Fatalf("resp %+v", resp)
	}
	if len(resp.References) != 3 || resp.References[0].Parent != "public.order_lines" {
		t.Fatalf("references %+v", resp.References)
	}
}
