// Command operator-plane serves the out-of-band operator plane (L-1).
//
// It is the same routes platform-api serves under /api/v1, built from the same
// packages, in a process that needs no cluster: no kubeconfig, no client-go, no
// internal/cluster. That is what lets it run beside the remediation runners on
// the Mac minis, where a bad platform-api release — or a dead cluster — cannot
// take away the surface you would use to repair either.
//
// It can own the patrol autopilot loop, but does not by default: platform-workers
// already runs one per environment, and exactly one process may. Set
// OPERATOR_PLANE_AUTOPILOT=on here only together with PLATFORM_ROLE=api on the
// platform-workers that currently runs it.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatorplane"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// 8781 is the remediation runner and 8782 the Hermes gateway on the same hosts.
const defaultListen = ":8783"

func main() {
	loadDotEnv()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	auth, err := actuation.LoadAuth(cfg.PlatformAuthPath)
	if err != nil {
		// Same posture as platform-api: an unreadable auth file leaves an empty
		// service, which denies operator routes rather than opening them.
		auth = &actuation.AuthService{}
		slog.Warn("operator plane auth not loaded, operator routes will deny", "err", err)
	}

	plane, err := operatorplane.New(operatorplane.Deps{
		Auth:      auth,
		Audit:     actuation.NewAuditLog(""),
		ConfigDir: cfg.ConfigDir(),
	})
	if err != nil {
		log.Fatalf("operator plane: %v", err)
	}

	autopilot := os.Getenv("OPERATOR_PLANE_AUTOPILOT") == "on"

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"bifrost-operator-plane","autopilot":` +
			btoa(autopilot) + `,"contained_panics":` + itoa(safego.Contained()) + `}`))
	})
	r.Route("/api/v1", plane.Mount)

	if autopilot {
		plane.StartBackground(context.Background())
	}

	listen := os.Getenv("OPERATOR_PLANE_LISTEN")
	if listen == "" {
		listen = defaultListen
	}
	slog.Info("bifrost-operator-plane listening",
		"addr", listen,
		"config", cfg.ConfigPath,
		"autopilot", autopilot,
		"note", "cluster-free by construction — see internal/operatorplane doc")
	if err := http.ListenAndServe(listen, r); err != nil {
		log.Fatalf("operator plane: %v", err)
	}
}

func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
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
