package main

import (
	"context"
	"fmt"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/platformcheck"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: agentrt-check rules|changed")
		os.Exit(2)
	}
	os.Exit(platformcheck.Run(context.Background(), ".", os.Args[1], os.Environ(), os.Stdout))
}
