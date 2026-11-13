// Command owner-exec runs approved owner_run_command cards inside the cluster.
//
// The Owner approves a card in the Console and types a one-time code from an
// authenticator app; the Console posts both here (internal/ownerexec has the
// protocol). The process holds one secret, the TOTP seed. It has no platform
// token of its own: each request carries the admin bearer of the page that
// sent it.
//
// Environment:
//
//	OWNER_EXEC_LISTEN        listen address (default :8787)
//	PLATFORM_API_URL         platform-api base URL (default http://platform-api:8780)
//	OWNER_EXEC_SEED_FILE     base32 TOTP seed, a mounted Secret key (default /etc/owner-exec/seed)
//	OWNER_EXEC_TEST_SEED     "generate": STG only, make a throwaway seed and log it;
//	                         refused when a cluster identity or a seed is mounted
//	OWNER_EXEC_TIMEOUT       longest a command may run (default 30m)
//	OWNER_EXEC_EGRESS_PROBE  host:port the NetworkPolicy must block; reachable = refuse to run
//	OWNER_EXEC_SHELL_URL     send commands to the shell container at this URL instead of
//	                         running them here (PROD: http://127.0.0.1:8788)
//
// `owner-exec shell` is the other container of the PROD Pod: it runs the
// commands, with the Pod's cluster identity, and holds no seed
// (OWNER_EXEC_SHELL_LISTEN, default 127.0.0.1:8788).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/ownerexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// version is the git SHA injected at build (-X main.version).
var version = "dev"

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "shell" {
		addr := env("OWNER_EXEC_SHELL_LISTEN", "127.0.0.1:8788")
		log.Printf("owner-exec shell %s listening on %s", version, addr)
		shell := &http.Server{Addr: addr, Handler: ownerexec.ShellHandler(log.Printf), ReadHeaderTimeout: 10 * time.Second}
		log.Fatal(shell.ListenAndServe())
	}
	started := time.Now()
	seed, err := ownerexec.LoadSeed(ownerexec.SeedConfig{
		SeedFile:  env("OWNER_EXEC_SEED_FILE", "/etc/owner-exec/seed"),
		TestMode:  os.Getenv(ownerexec.TestSeedEnv) == ownerexec.TestSeedValue,
		TokenFile: ownerexec.DefaultTokenFile,
		Issuer:    env("OWNER_EXEC_ISSUER", "Bifrost"),
		Account:   env("OWNER_EXEC_ACCOUNT", "owner-exec"),
	}, log.Printf)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	timeout, err := time.ParseDuration(env("OWNER_EXEC_TIMEOUT", "30m"))
	if err != nil || timeout <= 0 {
		log.Fatalf("OWNER_EXEC_TIMEOUT: not a positive duration")
	}
	host, _ := os.Hostname()
	srv := &ownerexec.Server{
		Platform:   ownerexec.NewHTTPPlatform(env("PLATFORM_API_URL", "http://platform-api:8780")),
		Verifier:   ownerexec.NewVerifier(seed, started),
		ExecutorID: "owner-exec:" + host,
		Timeout:    timeout,
		Logf:       log.Printf,
	}
	if shell := env("OWNER_EXEC_SHELL_URL", ""); shell != "" {
		srv.Run = ownerexec.RemoteRun(shell)
	}
	if probe := env("OWNER_EXEC_EGRESS_PROBE", ""); probe != "" {
		safego.Go("ownerexec.egress-probe", func() {
			time.Sleep(30 * time.Second)
			srv.ProbeEgress(probe, 5*time.Second)
		})
	}

	httpSrv := &http.Server{
		Addr:              env("OWNER_EXEC_LISTEN", ":8787"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
	}
	safego.Go("ownerexec.shutdown", func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	})
	log.Printf("owner-exec %s listening on %s; codes accepted from %s", version, httpSrv.Addr, started.Add(ownerexec.Warmup).UTC().Format(time.RFC3339))
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
	// Wait for known results to be acknowledged (delivery failures keep this
	// pending). A lost shell reply instead stops renewal so the card goes
	// unknown. Forced Pod termination can interrupt this in-memory drain.
	srv.Wait()
}
