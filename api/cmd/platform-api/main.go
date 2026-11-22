package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agentrt-migrate" {
		os.Exit(runAgentrtMigrate())
	}
	loadDotEnv()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Printf("bifrost-platform-api listening on %s (config: %s)", cfg.Listen, cfg.ConfigPath)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer srv.Close()
	handler := srv.Router()
	logRouteCoverage(handler)
	httpServer := &http.Server{Addr: cfg.Listen, Handler: handler}
	done := make(chan struct{})
	safego.Go("server.shutdown", func() {
		defer close(done)
		<-ctx.Done()
		// Stop taking requests and let the ones in flight finish first; then stop the
		// background loops and flush what this process still holds.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
		srv.Close()
	})
	serveErr := httpServer.ListenAndServe()
	stop()
	<-done
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatalf("server: %v", serveErr)
	}
}

func loadDotEnv() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	for _, p := range []string{
		filepath.Join(wd, ".env"),
		filepath.Join(wd, "..", ".env"),
	} {
		if err := godotenv.Load(p); err == nil {
			return
		}
	}
}

// logRouteCoverage logs each write route the action catalog and the exemption
// list do not account for (actions.CheckCovered). The route ratchet test fails
// on the same lines; this makes a drift visible in the running process too.
func logRouteCoverage(h http.Handler) {
	routes, ok := h.(chi.Routes)
	if !ok {
		return
	}
	var mounted []actions.MountedRoute
	_ = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted = append(mounted, actions.MountedRoute{Method: method, Pattern: route})
		return nil
	})
	problems, _ := actions.CheckCovered(mounted)
	for _, p := range problems {
		log.Printf("route coverage: %s", p)
	}
}
