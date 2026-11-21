package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
)

// The map's pool of what it cannot perceive (catalog, 0.2 map line,
// 2026-11-20): the workers scan main of each repository the CI scorecard
// knows every catalog.Every and keep the pool; GET /catalog/unperceived reads
// what was kept, never Gitea. This file hands catalog Gitea's tree reader.

func (s *Server) catalogScanner() *catalog.Scanner {
	s.catalogOnce.Do(func() {
		s.catalog = &catalog.Scanner{Source: s.catalogSource}
		s.catalog.AfterRound = func(ctx context.Context) { s.catalog.KeepStatus(ctx, s.catalogStatusInputs()) }
	})
	return s.catalog
}

func (s *Server) catalogSource(ctx context.Context, repo string) (catalog.Tree, error) {
	if repo == catalog.ClusterRepo {
		return s.catalogClusterTree(ctx)
	}
	if repo == catalog.DatabaseRepo {
		return s.catalogDatabaseTree(ctx)
	}
	if s.delivery == nil {
		return catalog.Tree{}, errors.New("this platform does not read Gitea")
	}
	commit, err := s.mainHead(ctx, repo)
	if err != nil {
		return catalog.Tree{}, err
	}
	tree, err := s.unitTreesOf()(ctx, repo, commit)
	if err != nil {
		return catalog.Tree{}, err
	}
	var files []string
	for _, e := range tree.Walk("") {
		files = append(files, e.Path)
	}
	return catalog.Tree{Commit: commit, Files: files, Read: func(p string) ([]byte, bool, error) {
		if e, ok := tree.Lookup(p); !ok || e.Dir {
			return nil, false, nil
		}
		raw, err := tree.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return raw, err == nil, err
	}}, nil
}

// catalogScanAhead starts the scanner in the workers: every repository of the
// Gitea organization the releases clone from (the tenant's own list: the
// platform names none of them), the CI scorecard's until the first listing.
func (s *Server) catalogScanAhead(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	fallback := make([]string, 0, len(ciscore.Repos))
	for _, r := range ciscore.Repos {
		fallback = append(fallback, r.Repo)
	}
	fallback = append(fallback, catalog.DatabaseRepo, catalog.ClusterRepo)
	s.catalogScanner().Run(ctx, func(ctx context.Context) ([]string, error) {
		repos, err := s.catalogRepos(ctx)
		if err != nil {
			return nil, err
		}
		// The database and the cluster are scanned last: their rows are the
		// platform table's, kept by that repository's scan earlier in the
		// same round.
		return append(repos, catalog.DatabaseRepo, catalog.ClusterRepo), nil
	}, fallback)
}

// catalogDatabaseTree lists the platform's own database's tables (every
// schema but the system ones, from pg_tables: it lists tables whatever the
// role may read) as "<schema>/<table>/table" files, against the kept
// "tables" rows. The tenants' databases are not read (Owner 2026-11-20).
func (s *Server) catalogDatabaseTree(ctx context.Context) (catalog.Tree, error) {
	if s.agentRuntime == nil || s.agentRuntime.late == nil || !s.agentRuntime.late.Ready() {
		return catalog.Tree{}, errors.New("the platform's database is not connected on this process")
	}
	rows, commit, ok := s.catalogScanner().TableRows()
	if !ok {
		return catalog.Tree{}, errors.New("no repository scanned yet has the ownership table's sections")
	}
	tx, err := s.agentRuntime.late.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return catalog.Tree{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rs, err := tx.Query(ctx, `SELECT schemaname, tablename FROM pg_catalog.pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema') AND schemaname NOT LIKE 'pg_toast%'`)
	if err != nil {
		return catalog.Tree{}, err
	}
	var files []string
	for rs.Next() {
		var schema, table string
		if err := rs.Scan(&schema, &table); err != nil {
			rs.Close()
			return catalog.Tree{}, err
		}
		files = append(files, schema+"/"+table+"/table")
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return catalog.Tree{}, err
	}
	sort.Strings(files)
	return catalog.WorkloadTree(commit, files, rows)
}

// catalogClusterTree lists what runs in the cluster (Deployments,
// StatefulSets, DaemonSets, CronJobs of every namespace, with the platform's
// cluster-wide read) as "<namespace>/<name>/<kind>" files, against the kept
// "workloads" rows.
func (s *Server) catalogClusterTree(ctx context.Context) (catalog.Tree, error) {
	if s.cluster == nil {
		return catalog.Tree{}, errors.New("this platform does not read a cluster")
	}
	rows, commit := s.catalogScanner().WorkloadRows()
	if rows == nil {
		return catalog.Tree{}, errors.New("no repository scanned yet has a workloads section")
	}
	kube, _, err := s.cluster.Service().KubernetesClient()
	if err != nil {
		return catalog.Tree{}, err
	}
	seen := map[string]bool{}
	health := map[string]catalog.Health{}
	add := func(ns, name, kind string) { seen[ns+"/"+name+"/"+kind] = true }
	judge := func(ns, name string, desired, ready int) {
		health[ns+"/"+name] = catalog.Health{Desired: desired, Ready: ready, Judged: true}
	}
	opts := metav1.ListOptions{}
	deps, err := kube.AppsV1().Deployments("").List(ctx, opts)
	if err != nil {
		return catalog.Tree{}, err
	}
	for _, o := range deps.Items {
		add(o.Namespace, o.Name, "deployment")
		judge(o.Namespace, o.Name, replicasOf(o.Spec.Replicas), int(o.Status.ReadyReplicas))
	}
	sts, err := kube.AppsV1().StatefulSets("").List(ctx, opts)
	if err != nil {
		return catalog.Tree{}, err
	}
	for _, o := range sts.Items {
		add(o.Namespace, o.Name, "statefulset")
		judge(o.Namespace, o.Name, replicasOf(o.Spec.Replicas), int(o.Status.ReadyReplicas))
	}
	dss, err := kube.AppsV1().DaemonSets("").List(ctx, opts)
	if err != nil {
		return catalog.Tree{}, err
	}
	for _, o := range dss.Items {
		add(o.Namespace, o.Name, "daemonset")
		judge(o.Namespace, o.Name, int(o.Status.DesiredNumberScheduled), int(o.Status.NumberReady))
	}
	cjs, err := kube.BatchV1().CronJobs("").List(ctx, opts)
	if err != nil {
		return catalog.Tree{}, err
	}
	for _, o := range cjs.Items {
		add(o.Namespace, o.Name, "cronjob")
		if _, ok := health[o.Namespace+"/"+o.Name]; !ok {
			health[o.Namespace+"/"+o.Name] = catalog.Health{}
		}
	}
	// The database: CNPG makes its pods directly, so none of the lists above
	// has it. Read with the platform's get/list on postgresql.cnpg.io; a
	// cluster without the operator (no such API) simply has none.
	if rc := kube.Discovery().RESTClient(); rc != nil {
		raw, err := rc.Get().AbsPath("/apis/postgresql.cnpg.io/v1/clusters").DoRaw(ctx)
		if err == nil {
			dbs, perr := catalog.ParseDatabaseClusters(raw)
			err = perr
			for _, d := range dbs {
				add(d.Namespace, d.Name, "postgres")
				judge(d.Namespace, d.Name, d.Instances, d.Ready)
			}
		}
		if err != nil {
			slog.Warn("catalog: list database clusters", "err", err)
		}
	}
	s.catalogHealth.Store(health)
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return catalog.WorkloadTree(commit, keys, rows)
}

// catalogRepos lists the organization's repositories that are neither
// archived nor empty, with the delivery service's read-only credential.
func (s *Server) catalogRepos(ctx context.Context) ([]string, error) {
	acc, err := gitAccess(s.delivery.GiteaAccess)(ctx)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var out []string
	for page := 1; page <= 10; page++ {
		u := strings.TrimRight(acc.Base, "/") + "/api/v1/orgs/" + url.PathEscape(acc.Org) + "/repos?limit=50&page=" + strconv.Itoa(page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if acc.User != "" {
			req.SetBasicAuth(acc.User, acc.Pass)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Name     string `json:"name"`
			Archived bool   `json:"archived"`
			Empty    bool   `json:"empty"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw)
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("gitea repos: status %d", resp.StatusCode)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			if !r.Archived && !r.Empty {
				out = append(out, r.Name)
			}
		}
		if len(raw) < 50 {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

// mountCatalog: the pool is read-only and names repositories and paths, so
// viewer and above.
func (s *Server) mountCatalog(r chi.Router) {
	viewer := r.With(s.auth.Require(actuation.RoleViewer))
	viewer.Method(http.MethodGet, "/catalog/unperceived", catalog.Route{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/owner", catalog.OwnerRoute{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/index", catalog.IndexRoute{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/status", catalog.StatusRoute{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/users", catalog.UsersRoute{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/domain", catalog.DomainRoute{Scanner: s.catalogScanner()})
}

func replicasOf(n *int32) int {
	if n == nil {
		return 1 // the API's default
	}
	return int(*n)
}

// catalogStatusInputs lends the map's status the cluster scan's replicas,
// the request counts Prometheus keeps of this platform's routes, and the
// domain graph the CI page keeps.
func (s *Server) catalogStatusInputs() catalog.StatusInputs {
	ns := k8sstate.Namespace()
	in := catalog.StatusInputs{
		Windows: []time.Duration{time.Hour, 24 * time.Hour},
		Hosts:   []string{ns + "/platform-api", ns + "/platform-workers"},
	}
	in.Health = func() map[string]catalog.Health {
		h, _ := s.catalogHealth.Load().(map[string]catalog.Health)
		return h
	}
	if prom := s.evalProm(); prom != nil {
		in.Requests = func(ctx context.Context, routes []string, window time.Duration) (map[string]catalog.RouteCount, error) {
			want := map[string]bool{}
			for _, r := range routes {
				want[r] = true
			}
			expr := fmt.Sprintf(`sum by (handler, status) (increase(http_requests_total{namespace=%q}[%dh]))`, ns, int(window.Hours()))
			samples, err := prom(ctx, expr)
			if err != nil {
				return nil, err
			}
			out := map[string]catalog.RouteCount{}
			for _, sm := range samples {
				h := sm.Labels["handler"]
				if !want[h] {
					continue
				}
				c := out[h]
				c.Total += sm.Value
				if sm.Labels["status"] == "5xx" {
					c.Errors += sm.Value
				}
				out[h] = c
			}
			return out, nil
		}
	}
	if s.delivery != nil {
		in.Boundary = func(ctx context.Context, repo string) ([]catalog.Edge, bool, error) {
			g, err := s.ciDomainsKept.Graph(ctx, s.ciDomainsSourceOf(), repo)
			if err != nil {
				return nil, false, err
			}
			edges := make([]catalog.Edge, 0, len(g.Edges))
			for _, e := range g.Edges {
				pairs := make([][2]string, 0, len(e.Pairs))
				for _, p := range e.Pairs {
					pairs = append(pairs, [2]string{p.From, p.To})
				}
				edges = append(edges, catalog.Edge{From: e.From, To: e.To, Imports: e.Imports, WrongWay: e.WrongWay,
					ToBase: g.Base != "" && e.To == g.Base, Pairs: pairs})
			}
			return edges, g.Measured, nil
		}
		in.Uses = s.catalogUses
	}
	return in
}
