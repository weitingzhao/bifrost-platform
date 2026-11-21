package releasepolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A signed policy may cover run_agentrt_migration (its ddl_reversible
// condition, Owner 2026-11-15): the platform runs, on its own environment,
// the migrations of a commit that environment has not applied. It is covered
// when the commit was delivered successfully to the source environment (the
// pipeline the policy pins this environment's pipeline to: that delivery ran
// the same files on the source database), and every file still to apply is
// under ddl_reversible_paths, new since the commit this environment runs,
// and of the class ClassifyMigration names. Until 2026-11-15 the Owner
// created the Job by hand (cards #493, #554, #558). A policy without the
// condition covers no migration, as before it existed.

const migrationAction = "run_agentrt_migration"

// Migrations reads what this platform knows of its own environment.
type Migrations interface {
	Own(ctx context.Context) (OwnMigrations, error)
}

// OwnMigrations is this platform's environment as the migration check reads
// it: the pipeline that delivers it, the repo that pipeline builds it from,
// where that repo keeps its migrations, and each migration the environment's
// database has applied (number -> sha256 of the file that ran).
type OwnMigrations struct {
	Pipeline, Repo, Dir string
	Applied             map[int]string
}

// MigrationState is a commit's migrations against what the environment has
// applied, compared the way the PROD deliver pipeline's schema task does: by
// number and by the sha256 of each file's bytes.
type MigrationState struct {
	Own OwnMigrations
	// Pending are the commit's migration files the environment has not
	// applied, as repo paths in order.
	Pending []string
	// Extra are migrations the environment has applied and the commit does
	// not carry (an older build).
	Extra []int
}

var migrationFile = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.sql$`)

// maxMigrationFiles bounds the files one comparison reads.
const maxMigrationFiles = 300

// PendingMigrations compares the migrations commit carries with the ones
// this environment has applied. An applied migration whose bytes differ at
// commit is an error: the migrator refuses such a file, and so does this.
func (e *Engine) PendingMigrations(ctx context.Context, commit string) (MigrationState, error) {
	if e.d.Migrations == nil || e.d.Git == nil {
		return MigrationState{}, fmt.Errorf("no reader for this environment's migrations or for Gitea")
	}
	own, err := e.d.Migrations.Own(ctx)
	if err != nil {
		return MigrationState{}, fmt.Errorf("this environment's migrations: %w", err)
	}
	st := MigrationState{Own: own}
	if own.Pipeline == "" || own.Repo == "" || own.Dir == "" || own.Applied == nil {
		return st, fmt.Errorf("this environment does not say which pipeline delivers it, from which repo, or what it has applied")
	}
	names, err := e.d.Git.ListDir(ctx, own.Repo, commit, own.Dir)
	if err != nil {
		return st, fmt.Errorf("cannot list %s/%s at %s: %w", own.Repo, own.Dir, short(commit), err)
	}
	sort.Strings(names)
	carried := map[int]string{}
	for _, name := range names {
		m := migrationFile.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		if prev, dup := carried[id]; dup {
			return st, fmt.Errorf("%s carries two migrations numbered %d (%s, %s)", short(commit), id, prev, name)
		}
		if carried[id] = name; len(carried) > maxMigrationFiles {
			return st, fmt.Errorf("%s carries more than the %d migrations this check reads", short(commit), maxMigrationFiles)
		}
		path := own.Dir + "/" + name
		text, found, err := e.d.Git.FileAt(ctx, own.Repo, commit, path)
		if err != nil || !found {
			return st, fmt.Errorf("cannot read %s at %s: %s", path, short(commit), errText(err))
		}
		sum := sha256.Sum256([]byte(text))
		switch ran, ok := own.Applied[id]; {
		case !ok:
			st.Pending = append(st.Pending, path)
		case ran != hex.EncodeToString(sum[:]):
			return st, fmt.Errorf("%s ran on this environment with other bytes than %s carries (edited after it ran)", name, short(commit))
		}
	}
	if len(carried) == 0 {
		return st, fmt.Errorf("%s carries no migrations under %s", short(commit), own.Dir)
	}
	for id := range own.Applied {
		if _, ok := carried[id]; !ok {
			st.Extra = append(st.Extra, id)
		}
	}
	sort.Ints(st.Extra)
	return st, nil
}

// SourcePipeline is the pipeline the signed policy pins pipeline to: where a
// commit must have been delivered before this environment takes it. ok is
// false when no policy with a good signature pins it.
func (e *Engine) SourcePipeline(ctx context.Context, pipeline string) (from string, ok bool) {
	st := e.Status(ctx)
	if st.policy == nil {
		return "", false
	}
	from = st.policy.Pinned[pipeline].From
	return from, from != ""
}

// evaluateMigration judges one run_agentrt_migration request. Without the
// ddl_reversible condition in a signed policy the answer is the one every
// non-release action gets.
func (e *Engine) evaluateMigration(ctx context.Context, params map[string]any) Decision {
	d := Decision{SHAs: map[string]string{}}
	st := e.Status(ctx)
	if st.policy == nil || !st.policy.DDLReversible() {
		d.Reasons = []string{migrationAction + " is not a release; the policy covers " + releaseAction + " only"}
		return d
	}
	pol := st.policy
	// The policy id is set from here on, covered or not: it is how a caller
	// tells a policy that judges migrations from one that does not.
	d.PolicyID = pol.PolicyID
	if st.Frozen {
		d.Reasons = []string{"releases are frozen: " + st.FreezeReason + " (lift: " + UnfreezeCommand + ")"}
		return d
	}
	if !st.Valid {
		d.Reasons = append(append([]string{}, st.Reasons...), "sign a policy: "+SignCommand)
		return d
	}
	d.Clauses = append(d.Clauses, "signature", "unexpired", "not_frozen")
	commit := str(params["commit"])
	if !fullSHA.MatchString(commit) {
		d.Reasons = []string{"the request names no full commit id"}
		return d
	}
	if e.d.Deployed == nil {
		d.Reasons = []string{"no reader for deployed commits"}
		return d
	}
	state, err := e.PendingMigrations(ctx, commit)
	own := state.Own
	d.Pipeline = own.Pipeline
	if own.Repo != "" {
		d.SHAs[own.Repo] = commit
	}
	if err != nil {
		d.Reasons = []string{err.Error()}
		return d
	}
	if len(state.Extra) > 0 {
		// The migrator refuses to run for a build that lacks an applied file.
		d.Reasons = []string{fmt.Sprintf("this environment has applied migrations %v, which %s does not carry (an older build)", state.Extra, short(commit))}
		return d
	}
	if !pol.Allows(own.Pipeline) {
		d.Reasons = []string{own.Pipeline + ", which delivers this environment, is not in the policy's allow list"}
		return d
	}
	d.Clauses = append(d.Clauses, "allow:"+own.Pipeline)
	pin := pol.Pinned[own.Pipeline]
	pinsRepo := false
	for _, repo := range pin.Params {
		pinsRepo = pinsRepo || repo == own.Repo
	}
	if pin.From == "" || !pinsRepo {
		d.Reasons = []string{"the policy pins " + own.Pipeline + " to no source pipeline for " + own.Repo + ", so nothing says where the commit must have been delivered first"}
		return d
	}
	var reasons []string
	if why := e.deliveredBy(ctx, pin.From, map[string]string{own.Repo: commit}); why != "" {
		reasons = append(reasons, why)
	} else {
		d.Clauses = append(d.Clauses, "pinned_to:"+pin.From)
	}
	deployed, _, err := e.d.Deployed.Deployed(ctx, own.Pipeline)
	old := deployed[own.Repo]
	switch {
	case err != nil:
		reasons = append(reasons, "deployed commits for "+own.Pipeline+": "+err.Error())
	case old == "":
		reasons = append(reasons, "no release record of "+own.Pipeline+" says which commit of "+own.Repo+" this environment runs, so no file can be shown to be new")
	default:
		inside := pol.reversiblePaths(own.Repo, state.Pending)
		if outside := without(state.Pending, inside); len(outside) > 0 {
			reasons = append(reasons, "pending migrations outside the policy's ddl_reversible_paths: "+listed(outside))
		}
		if why := e.reversibleDDL(ctx, own.Repo, old, commit, inside); why != "" {
			reasons = append(reasons, fmt.Sprintf("%s %s..%s has a pending migration that is not a new file that cannot destroy rows (ask the Owner): %s",
				own.Repo, short(old), short(commit), why))
		}
		if len(reasons) == 0 {
			d.Clauses = append(d.Clauses, "ddl_reversible")
			for _, path := range state.Pending {
				d.ReversibleDDL = append(d.ReversibleDDL, own.Repo+":"+path)
			}
		}
	}
	d.Reasons = reasons
	d.Auto = len(reasons) == 0
	return d
}

// appliedHere is the part of files (migration paths in repo's diff) that the
// environment this platform runs has already applied with the bytes next
// carries, when pipeline is the one that delivers this environment. Such a
// file runs nothing on release: the PROD pipeline's schema task finds it
// applied. Origin: 2026-11-21, card #674 — the release of 3c335db2..cc59bf9f
// needed the Owner only because 0042 (a DROP) was in the diff, though card
// #671 had already run 0042 on PROD (applied=[42]): one deletion approved
// twice. Any read that fails, or any other pipeline, gives nothing, and the
// files are judged as before.
func (e *Engine) appliedHere(ctx context.Context, pipeline, repo, next string, files []string) []string {
	if e.d.Migrations == nil || e.d.Git == nil || pipeline == "" {
		return nil
	}
	own, err := e.d.Migrations.Own(ctx)
	if err != nil || own.Pipeline != pipeline || own.Repo != repo || own.Dir == "" || own.Applied == nil {
		return nil
	}
	var out []string
	for _, path := range files {
		dir, name := path, ""
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir, name = path[:i], path[i+1:]
		}
		m := migrationFile.FindStringSubmatch(name)
		if dir != own.Dir || m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		ran, ok := own.Applied[id]
		if !ok {
			continue
		}
		text, found, err := e.d.Git.FileAt(ctx, repo, next, path)
		if err != nil || !found {
			continue
		}
		h := sha256.Sum256([]byte(text))
		if hex.EncodeToString(h[:]) == ran {
			out = append(out, path)
		}
	}
	return out
}
