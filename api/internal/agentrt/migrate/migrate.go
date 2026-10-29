// Package migrate applies the agentrt migrations: forward only, one
// transaction per file, the sha256 of every applied file recorded and
// re-checked, and a session advisory lock so two migrators started together
// run one after the other (design v5 §5.1.1, T19).
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrations"
)

// lockKey is the advisory lock every migrator takes before reading state.
const lockKey int64 = 7421000000

var fileName = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.sql$`)

// ErrEdited means an applied migration file no longer has the bytes it had
// when it ran; nothing is applied until it is restored.
var ErrEdited = errors.New("migrate: an applied migration file was changed")

// File is one migration.
type File struct {
	ID     int
	Name   string
	SQL    string
	SHA256 string
}

// Files lists the embedded migrations in order.
func Files() ([]File, error) {
	return filesFrom(migrations.FS)
}

func filesFrom(fsys fs.FS) ([]File, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []File
	seen := map[int]string{}
	for _, e := range entries {
		m := fileName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		if prev, dup := seen[id]; dup {
			return nil, fmt.Errorf("migrate: %s and %s share number %d", prev, e.Name(), id)
		}
		seen[id] = e.Name()
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(b)
		out = append(out, File{ID: id, Name: e.Name(), SQL: string(b), SHA256: hex.EncodeToString(h[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i, f := range out {
		if f.ID != i+1 {
			return nil, fmt.Errorf("migrate: expected migration %04d, found %s", i+1, f.Name)
		}
	}
	return out, nil
}

// RuntimeRole is the role the grants migration names for an environment.
func RuntimeRole(env string) (string, error) {
	switch env {
	case "dev", "stg", "prod":
		return "platform_agentrt_" + env, nil
	}
	return "", fmt.Errorf("migrate: environment %q is not dev, stg or prod", env)
}

// Run applies every migration not yet applied, on one connection that holds
// the advisory lock for the whole run. It returns the numbers it applied.
func Run(ctx context.Context, conn *pgx.Conn, env string) ([]int, error) {
	return RunFiles(ctx, conn, env, nil)
}

// RunFiles is Run over a given list (nil = the embedded files); tests use it
// to show that an edited file is refused.
func RunFiles(ctx context.Context, conn *pgx.Conn, env string, files []File) ([]int, error) {
	role, err := RuntimeRole(env)
	if err != nil {
		return nil, err
	}
	if files == nil {
		if files, err = Files(); err != nil {
			return nil, err
		}
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return nil, fmt.Errorf("migrate: lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
	}()
	if _, err = conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS agentrt`); err != nil {
		return nil, fmt.Errorf("migrate: schema: %w", err)
	}
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS agentrt.schema_migration (
	  schema_migration_id int PRIMARY KEY,
	  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
	  applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("migrate: bookkeeping table: %w", err)
	}
	applied := map[int]string{}
	rows, err := conn.Query(ctx, `SELECT schema_migration_id, sha256 FROM agentrt.schema_migration`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		var sum string
		if err := rows.Scan(&id, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		applied[id] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	known := map[int]bool{}
	for _, f := range files {
		known[f.ID] = true
		if sum, ok := applied[f.ID]; ok && sum != f.SHA256 {
			return nil, fmt.Errorf("%w: %s ran with sha256 %s, the file now has %s", ErrEdited, f.Name, sum, f.SHA256)
		}
	}
	for id := range applied {
		if !known[id] {
			return nil, fmt.Errorf("migrate: migration %04d is applied but this binary does not have it", id)
		}
	}
	var did []int
	for _, f := range files {
		if _, ok := applied[f.ID]; ok {
			continue
		}
		if err := apply(ctx, conn, role, f); err != nil {
			return did, fmt.Errorf("migrate: %s: %w", f.Name, err)
		}
		did = append(did, f.ID)
	}
	return did, nil
}

func apply(ctx context.Context, conn *pgx.Conn, role string, f File) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Bound every file before it can acquire DDL locks. A blocked ALTER must
	// not hold earlier locks for the CLI's ten-minute context. Two seconds
	// lets short transactions finish; longer traffic requires a later retry.
	// Thirty seconds bounds each statement (including CHECK/index scans).
	// These transaction-local settings also cover the migration record and
	// leave the caller's session settings intact after commit or rollback.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'; SET LOCAL statement_timeout = '30s'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('agentrt.runtime_role', $1, true)`, role); err != nil {
		return err
	}
	// The file runs as one simple-protocol batch, so it may hold several
	// statements and dollar-quoted function bodies.
	if _, err := tx.Exec(ctx, f.SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.schema_migration (schema_migration_id, sha256) VALUES ($1, $2)`,
		f.ID, f.SHA256); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
