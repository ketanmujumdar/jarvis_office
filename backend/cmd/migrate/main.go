// Command migrate applies SQL migrations and optionally loads seed data.
//
//	go run ./cmd/migrate          # migrations only
//	go run ./cmd/migrate -seed    # migrations + seed (idempotent; keeps admin edits)
//	go run ./cmd/migrate -seed -overwrite  # also re-apply vendors/catalog/addresses/policy from seed files
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func main() {
	doSeed := flag.Bool("seed", false, "load seed data after migrating")
	overwritePolicy := flag.Bool("overwrite-policy", false, "replace policy row from seed/policy.yaml")
	overwrite := flag.Bool("overwrite", false, "upsert vendors, catalog, addresses and policy from seed files")
	flag.Parse()
	if err := run(*doSeed, seed.Options{OverwritePolicy: *overwritePolicy, Overwrite: *overwrite}); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(doSeed bool, opts seed.Options) error {
	// Keys are not needed for migrations.
	os.Setenv("FAKES", "true")
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := store.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Println("applied migrations:", applied)
	if doSeed {
		if err := seed.Load(ctx, postgres.New(pool), cfg.SeedDir, opts); err != nil {
			return err
		}
		fmt.Println("seed loaded from", cfg.SeedDir)
	}
	return nil
}
