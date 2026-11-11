// Command agentrt-baseline-import imports the immutable 2026-11-09 baseline.
// AGENTRT_DATABASE_URL must name the migrated runtime database and runtime role.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	url := os.Getenv("AGENTRT_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("AGENTRT_DATABASE_URL is required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect to runtime database failed")
	}
	defer conn.Close(ctx)
	n, err := store.ImportBaseline1109(ctx, conn)
	if err != nil {
		return err
	}
	fmt.Printf("Imported %d baseline readings\n", n)
	return nil
}
