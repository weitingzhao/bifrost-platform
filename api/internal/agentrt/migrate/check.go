package migrate

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The schema a running platform finds (TD-411: STG ran platform d360a118,
// which needs 0007 and 0008, on a database still at 0006 from 2026-10-20
// 23:24 CDT; the controller's facts pass failed every 15 seconds on a check
// constraint 0007 replaces, logged as a warning, and health said ok). The
// platform reads which migrations the database has applied and says how that
// stands against the ones it carries: health, Progress and the start log say
// so. It never applies one: that is the agentrt-migrate Job's, as the
// migration owner.

// Schema states, from the worst.
const (
	SchemaEdited  = "edited"  // an applied migration ran with other bytes than this binary's file: the migrator refuses to run
	SchemaBehind  = "behind"  // the database misses migrations this binary carries
	SchemaAhead   = "ahead"   // the database has migrations this binary does not carry (an older binary)
	SchemaCurrent = "current" // the database has applied exactly this binary's migrations
)

// SchemaStatus is the database's agentrt schema against the migrations this
// binary carries.
type SchemaStatus struct {
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"` // this binary's migrations the database has not applied, by file name
	Unknown []int    `json:"unknown,omitempty"` // applied migrations this binary does not carry
	Edited  []string `json:"edited,omitempty"`  // applied with other bytes than this binary's file
	// Applied is every migration the database has applied, by number: what a
	// release of another binary is checked against before it rolls out
	// (TD-411, the PROD deliver pipeline), whatever this binary carries.
	Applied []Applied `json:"applied"`
}

// Applied is a migration the database has applied: its number and the
// sha256 of the file it ran.
type Applied struct {
	ID     int    `json:"id"`
	SHA256 string `json:"sha256"`
}

// Querier is what Check reads with: a connection, a pool or a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

var embedded = sync.OnceValues(Files)

// Check reads the migrations the database has applied and says how they
// stand against this binary's. A database with no agentrt schema or no
// bookkeeping table has applied none. It writes nothing; the runtime's role
// may read the table (0003_grants).
func Check(ctx context.Context, q Querier) (SchemaStatus, error) {
	files, err := embedded()
	if err != nil {
		return SchemaStatus{}, err
	}
	applied := map[int]string{}
	rows, err := q.Query(ctx, `SELECT schema_migration_id, sha256 FROM agentrt.schema_migration`)
	if err == nil {
		for rows.Next() {
			var id int
			var sum string
			if err = rows.Scan(&id, &sum); err != nil {
				break
			}
			applied[id] = sum
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "42P01" || pg.Code == "3F000") {
		err = nil // no table, or no schema: none applied
	}
	if err != nil {
		return SchemaStatus{}, err
	}
	return compare(files, applied), nil
}

// compare is the status of applied (id → sha256) against files.
func compare(files []File, applied map[int]string) SchemaStatus {
	var s SchemaStatus
	carried := map[int]bool{}
	for _, f := range files {
		carried[f.ID] = true
		sum, ok := applied[f.ID]
		switch {
		case !ok:
			s.Missing = append(s.Missing, f.Name[:len(f.Name)-len(".sql")])
		case sum != f.SHA256:
			s.Edited = append(s.Edited, f.Name[:len(f.Name)-len(".sql")])
		}
	}
	s.Applied = []Applied{}
	for id, sum := range applied {
		if !carried[id] {
			s.Unknown = append(s.Unknown, id)
		}
		s.Applied = append(s.Applied, Applied{ID: id, SHA256: sum})
	}
	sort.Ints(s.Unknown)
	sort.Slice(s.Applied, func(i, j int) bool { return s.Applied[i].ID < s.Applied[j].ID })
	switch {
	case len(s.Edited) > 0:
		s.State = SchemaEdited
	case len(s.Missing) > 0:
		s.State = SchemaBehind
	case len(s.Unknown) > 0:
		s.State = SchemaAhead
	default:
		s.State = SchemaCurrent
	}
	return s
}
