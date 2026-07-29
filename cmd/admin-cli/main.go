package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/database"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "migrate":
		return migrate(args[1:])
	case "--help", "-h", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func migrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	databaseURL := fs.String("database-url", firstNonEmpty(os.Getenv("THIRDSHIFT_DATABASE_URL"), os.Getenv("DATABASE_URL")), "PostgreSQL connection string")
	migrationsDir := fs.String("migrations-dir", "migrations", "directory containing *.up.sql migrations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *databaseURL == "" {
		return fmt.Errorf("database URL is required; set THIRDSHIFT_DATABASE_URL or DATABASE_URL, or pass --database-url")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applied, err := database.ApplyURL(ctx, *databaseURL, *migrationsDir)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(os.Stdout, "migrations: no changes")
		return nil
	}
	for _, migration := range applied {
		fmt.Fprintf(os.Stdout, "migrations: applied %s %s\n", migration.Version, migration.Name)
	}
	return nil
}

func usage() error {
	fmt.Fprint(os.Stderr, usageText())
	return nil
}

func usageText() string {
	return "admin-cli commands:\n  migrate [--database-url URL] [--migrations-dir migrations]\n"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
