package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// What deploy_agentd (agentdeploy/agentd.go) and its proposal
// (controller/agentddeploy.go) read and write of a host: it is set aside
// with a drain control as the Console's set-aside is (so it claims nothing,
// and the presence pass counts it as taken out of work), set back by
// clearing that control, the attempts that run on it counted as the hosts
// document counts them, and the agentd build its claims report read from the
// head of its build stream (build:host:<host>, whose notes carry
// agentd_build {commit, sha256, modified}).

// agentdBuildStream is a host's build stream.
func agentdBuildStream(host string) string { return "build:host:" + host }

// SetHostAside sets a drain control on host as the controller, with reason,
// and returns its id.
func SetHostAside(ctx context.Context, db DB, host, reason string) (string, error) {
	if host == "" || reason == "" {
		return "", invalid("a host and a reason are required")
	}
	id := NewControlID()
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, scopeRows["host"], host).Scan(&one); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("there is no host %s", host)
			}
			return err
		}
		return SetControl(ctx, tx, "controller", id, "host", host, "drain", reason)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// SetHostBack clears the control SetHostAside set, as the controller; one
// already cleared (the Owner lifted it) is left as it is.
func SetHostBack(ctx context.Context, db DB, controlID string) error {
	_, err := LiftControl(ctx, db, "controller", controlID)
	return err
}

// HostLiveAttempts counts the attempts leased, running or stale on host.
func HostLiveAttempts(ctx context.Context, db DB, host string) (int, error) {
	var n int
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE host_id = $1 AND state IN ('leased','running','stale')`, host).Scan(&n)
	})
	return n, err
}

var hostAgentdCommitSQL = `SELECT ` + headSQL("$1::text") + `->'body'->'agentd_build'->>'commit'`

// HostAgentdCommit is the commit of the agentd build host's claims last
// reported, "" while none is recorded.
func HostAgentdCommit(ctx context.Context, db DB, host string) (string, error) {
	var commit *string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, hostAgentdCommitSQL, agentdBuildStream(host)).Scan(&commit)
	})
	if err != nil || commit == nil {
		return "", err
	}
	return *commit, nil
}
