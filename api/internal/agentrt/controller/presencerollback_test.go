package controller_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
)

// Review 530 B-1 (the reviewer's TestReview530RollbackOutlivesPassDeadline,
// adapted): a real pgx connection over net.Pipe to a peer that speaks just
// enough of PostgreSQL's protocol to fail the pass's read of the latest claim
// with SQLSTATE 55P03, then leaves the ROLLBACK unanswered. The pass, its
// limit 50 ms, returns with that error once the rollback's own bound has
// passed, not when the peer answers; the connection is closed, and no
// goroutine of the pass is left waiting.
func TestAPassWhoseRollbackGetsNoAnswerReturns(t *testing.T) {
	defer controller.SetPresencePassLimitForTest(50 * time.Millisecond)()
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	rollback := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		b := pgproto3.NewBackend(peer, peer)
		if _, err := b.ReceiveStartupMessage(); err != nil {
			return
		}
		b.Send(&pgproto3.AuthenticationOk{})
		b.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "17.0"})
		b.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
		b.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
		b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if b.Flush() != nil {
			return
		}
		for {
			msg, err := b.Receive()
			if err != nil {
				return
			}
			q, ok := msg.(*pgproto3.Query)
			if !ok {
				return
			}
			switch {
			case strings.HasPrefix(q.String, "begin"):
				b.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
				b.Send(&pgproto3.ReadyForQuery{TxStatus: 'T'})
			case strings.Contains(q.String, "SELECT max(last_poll_at)"):
				b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "55P03", Message: "controlled statement error"})
				b.Send(&pgproto3.ReadyForQuery{TxStatus: 'E'})
			case q.String == "rollback":
				close(rollback)
				<-release // never answered while the test looks
				return
			default:
				panic(fmt.Sprintf("unexpected query %q", q.String))
			}
			if b.Flush() != nil {
				return
			}
		}
	}()
	cfg, err := pgx.ParseConfig("postgres://test@offline.invalid/test?sslmode=disable")
	must(t, err)
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.LookupFunc = func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil }
	cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) { return client, nil }
	cctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	conn, err := pgx.ConnectConfig(cctx, cfg)
	must(t, err)
	c := &controller.Controller{DB: conn, PlatformUp: func(context.Context) error { return errors.New("connect: connection refused") }}
	start := time.Now()
	done := make(chan error, 1)
	go func() { _, err := c.Presence(context.Background()); done <- err }()
	select {
	case <-rollback:
	case <-time.After(time.Second):
		t.Fatal("the rollback was not reached")
	}
	select {
	case err := <-done:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("the pass returned %v; want the statement's error", err)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Fatalf("the pass took %v; its limit and the rollback's are 50 ms and 2 s", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pass is still waiting on a rollback that gets no answer")
	}
	if !conn.IsClosed() {
		t.Fatal("the connection whose rollback got no answer was kept")
	}
}
