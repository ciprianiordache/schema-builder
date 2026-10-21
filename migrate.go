package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// migrationLockKey is the pg_advisory_lock key that serialises concurrent
// migration runs (two app instances starting at once).
const migrationLockKey = 7_301_115_990

// migrationFile names follow golang-migrate's convention, so the same folder
// can be driven by the migrate CLI if that is ever preferred:
//
//	0001_tenancy.up.sql / 0001_tenancy.down.sql
var migrationFile = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.(up|down)\.sql$`)

// Migration is one numbered step with its forward and backward SQL.
type Migration struct {
	Version int64
	Name    string
	Up      string
	Down    string
}

// LoadMigrations reads every migration in fsys (a directory of .sql files;
// other files, such as an embed.go, are skipped) and returns them in version
// order. A version with an up file but no down file, a duplicate version or
// a misnamed .sql file is an error: a migration set that cannot be rolled
// back is caught before it reaches a database.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: read dir: %w", err)
	}
	byVersion := map[int64]*Migration{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := migrationFile.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migrations: unexpected file %q", e.Name())
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrations: read %s: %w", e.Name(), err)
		}
		mig := byVersion[v]
		if mig == nil {
			mig = &Migration{Version: v, Name: m[2]}
			byVersion[v] = mig
		} else if mig.Name != m[2] {
			return nil, fmt.Errorf("migrations: version %d has two names (%s, %s)", v, mig.Name, m[2])
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migrations: %04d_%s needs both an up and a down file", m.Version, m.Name)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrate applies every pending migration in fsys, each in its own
// transaction, under an advisory lock, and returns the ones it applied.
// PostgreSQL only.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS) ([]Migration, error) {
	migs, err := LoadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	var applied []Migration
	err = withMigrationLock(ctx, db, func(conn *sql.Conn) error {
		done, err := appliedVersions(ctx, conn)
		if err != nil {
			return err
		}
		for _, m := range migs {
			if done[m.Version] {
				continue
			}
			if err := runInTx(ctx, conn, m.Up,
				`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
				return fmt.Errorf("migrations: up %04d_%s: %w", m.Version, m.Name, err)
			}
			applied = append(applied, m)
		}
		return nil
	})
	return applied, err
}

// Rollback undoes the last steps applied migrations, newest first, and
// returns the ones it rolled back.
func Rollback(ctx context.Context, db *sql.DB, fsys fs.FS, steps int) ([]Migration, error) {
	migs, err := LoadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	var rolled []Migration
	err = withMigrationLock(ctx, db, func(conn *sql.Conn) error {
		done, err := appliedVersions(ctx, conn)
		if err != nil {
			return err
		}
		for i := len(migs) - 1; i >= 0 && len(rolled) < steps; i-- {
			m := migs[i]
			if !done[m.Version] {
				continue
			}
			if err := runInTx(ctx, conn, m.Down,
				`DELETE FROM schema_migrations WHERE version = $1`, m.Version); err != nil {
				return fmt.Errorf("migrations: down %04d_%s: %w", m.Version, m.Name, err)
			}
			rolled = append(rolled, m)
		}
		return nil
	})
	return rolled, err
}

// withMigrationLock pins one connection (advisory locks are per session),
// takes the lock, makes sure schema_migrations exists, then runs fn.
func withMigrationLock(ctx context.Context, db *sql.DB, fn func(conn *sql.Conn) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("migrations: lock: %w", err)
	}
	defer func() {
		_, uerr := conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
		err = errors.Join(err, uerr)
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    bigint      PRIMARY KEY,
			name       text        NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("migrations: create schema_migrations: %w", err)
	}
	return fn(conn)
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[int64]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrations: list applied: %w", err)
	}
	defer rows.Close()
	done := map[int64]bool{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		done[v] = true
	}
	return done, rows.Err()
}

// runInTx executes a migration body plus its bookkeeping statement
// atomically, so a failed migration leaves neither schema nor record behind.
func runInTx(ctx context.Context, conn *sql.Conn, body, record string, args ...any) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, record, args...); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
