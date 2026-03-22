package database

import (
	"embed"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// loadMigrations reads all SQL files from the embedded migrations/ directory,
// sorted by filename (001_xxx.sql, 002_xxx.sql, ...).
func loadMigrations() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	sqls := make([]string, 0, len(entries))
	for _, e := range entries {
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		sqls = append(sqls, string(data))
	}
	return sqls, nil
}
