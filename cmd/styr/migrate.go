package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
)

// runMigrate loads the configuration, opens the database (which applies
// every pending goose migration as a side effect of db.Open) and exits.
func runMigrate(stdout io.Writer) int {
	cfg, err := config.Load(os.Getenv("STYR_CONFIG"))
	if err != nil {
		fmt.Fprintf(stdout, "migrate: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		fmt.Fprintf(stdout, "migrate: %v\n", err)
		return 1
	}
	d, err := db.Open(cfg.DBPath())
	if err != nil {
		fmt.Fprintf(stdout, "migrate: %v\n", err)
		return 1
	}
	defer func() { _ = d.Close() }()

	fmt.Fprintln(stdout, "migrations applied")
	return 0
}
