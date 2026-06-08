package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/server"
)

func main() {
	loadDotEnv()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	srv := server.New(cfg)
	log.Printf("bifrost-platform-api listening on %s (config: %s)", cfg.Listen, cfg.ConfigPath)
	if err := http.ListenAndServe(cfg.Listen, srv.Router()); err != nil {
		log.Fatalf("server: %v", err)
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
