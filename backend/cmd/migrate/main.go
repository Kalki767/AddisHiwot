// Command migrate applies the database migrations in backend/migrations.
//
//	migrate up            apply every pending migration
//	migrate down [N|all]  roll back N migrations (default 1) or all of them
//	migrate version       print the current version
//	migrate force V       mark version V as applied (repair after a failed migration)
//
// The database comes from DATABASE_URL.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/database"
)

const usage = "usage: migrate up | down [N|all] | version | force V"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	m, err := database.NewMigrator(url)
	if err != nil {
		return err
	}
	defer m.Close()

	switch cmd, rest := args[0], args[1:]; {
	case cmd == "up" && len(rest) == 0:
		err = m.Up()
	case cmd == "down" && len(rest) == 0:
		err = m.Down(1)
	case cmd == "down" && len(rest) == 1 && rest[0] == "all":
		err = m.DownAll()
	case cmd == "down" && len(rest) == 1:
		n, convErr := strconv.Atoi(rest[0])
		if convErr != nil {
			return errors.New(usage)
		}
		err = m.Down(n)
	case cmd == "force" && len(rest) == 1:
		v, convErr := strconv.Atoi(rest[0])
		if convErr != nil {
			return errors.New(usage)
		}
		err = m.Force(v)
	case cmd == "version" && len(rest) == 0:
	default:
		return errors.New(usage)
	}
	if err != nil {
		return err
	}

	version, dirty, err := m.Version()
	if err != nil {
		return err
	}
	fmt.Printf("database at migration %d (dirty: %t)\n", version, dirty)
	return nil
}
