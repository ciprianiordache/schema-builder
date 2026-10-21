package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// snapshot is what PostgreSQL itself says one schema contains. Comparing two
// snapshots needs no SQL parsing: Postgres has already normalised every type,
// default, CHECK and foreign key into the same text on both sides.
type snapshot struct {
	tables     map[string]*tableSnap
	order      []string        // table names, sorted
	extensions map[string]bool // installed in the database, any schema
}

type tableSnap struct {
	name   string
	cols   []*colSnap
	byName map[string]*colSnap
	cons   map[string]*conSnap
	idx    map[string]string // index name -> definition (schema stripped)
}

type colSnap struct {
	name      string
	typ       string // format_type
	notNull   bool
	identity  string // "", "a" (ALWAYS), "d" (BY DEFAULT)
	generated string // "", "s" (STORED)
	expr      string // default, or the generation expression
}

type conSnap struct {
	name string
	kind string // p, u, f, c, x
	def  string // pg_get_constraintdef
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readSnapshot reads schema ns. Run it with search_path starting at ns, so
// references between its own tables print unqualified.
func readSnapshot(ctx context.Context, q querier, ns string) (*snapshot, error) {
	exts, err := readExtensions(ctx, q)
	if err != nil {
		return nil, err
	}
	s := &snapshot{tables: map[string]*tableSnap{}, extensions: exts}

	rows, err := q.QueryContext(ctx, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
		       a.attidentity::text, a.attgenerated::text, coalesce(pg_get_expr(d.adbin, d.adrelid), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')
		ORDER BY c.relname, a.attnum`, ns)
	if err != nil {
		return nil, fmt.Errorf("schema: read columns of %s: %w", ns, err)
	}
	err = eachRow(rows, func() error {
		var table string
		col := &colSnap{}
		if err := rows.Scan(&table, &col.name, &col.typ, &col.notNull, &col.identity, &col.generated, &col.expr); err != nil {
			return err
		}
		t := s.tables[table]
		if t == nil {
			t = &tableSnap{name: table, byName: map[string]*colSnap{}, cons: map[string]*conSnap{}, idx: map[string]string{}}
			s.tables[table] = t
			s.order = append(s.order, table)
		}
		t.cols = append(t.cols, col)
		t.byName[col.name] = col
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schema: read columns of %s: %w", ns, err)
	}

	// NOT NULL is read from attnotnull; PostgreSQL 18 also lists it as
	// contype 'n', which is left out here.
	rows, err = q.QueryContext(ctx, `
		SELECT c.relname, k.conname, k.contype::text, pg_get_constraintdef(k.oid)
		FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND k.contype IN ('p', 'u', 'f', 'c', 'x')`, ns)
	if err != nil {
		return nil, fmt.Errorf("schema: read constraints of %s: %w", ns, err)
	}
	err = eachRow(rows, func() error {
		var table string
		con := &conSnap{}
		if err := rows.Scan(&table, &con.name, &con.kind, &con.def); err != nil {
			return err
		}
		if t := s.tables[table]; t != nil {
			t.cons[con.name] = con
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schema: read constraints of %s: %w", ns, err)
	}

	// Indexes that back a PRIMARY KEY / UNIQUE / EXCLUDE constraint are
	// covered by the constraint itself.
	rows, err = q.QueryContext(ctx, `
		SELECT t.relname, i.relname, pg_get_indexdef(x.indexrelid)
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND t.relkind IN ('r', 'p')
		  AND NOT EXISTS (SELECT 1 FROM pg_constraint k
		                  WHERE k.conindid = x.indexrelid AND k.contype IN ('p', 'u', 'x'))`, ns)
	if err != nil {
		return nil, fmt.Errorf("schema: read indexes of %s: %w", ns, err)
	}
	prefix := quoteIdent(ns) + "."
	err = eachRow(rows, func() error {
		var table, name, def string
		if err := rows.Scan(&table, &name, &def); err != nil {
			return err
		}
		def = strings.Replace(def, " ON ONLY "+prefix, " ON ONLY ", 1)
		def = strings.Replace(def, " ON "+prefix, " ON ", 1)
		if t := s.tables[table]; t != nil {
			t.idx[name] = def
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schema: read indexes of %s: %w", ns, err)
	}
	return s, nil
}

// readExtensions lists the extensions installed in the database. They are
// database-wide, not per schema.
func readExtensions(ctx context.Context, q querier) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT extname FROM pg_extension`)
	if err != nil {
		return nil, fmt.Errorf("schema: read extensions: %w", err)
	}
	exts := map[string]bool{}
	err = eachRow(rows, func() error {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		exts[name] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schema: read extensions: %w", err)
	}
	return exts, nil
}

func eachRow(rows *sql.Rows, fn func() error) error {
	defer rows.Close()
	for rows.Next() {
		if err := fn(); err != nil {
			return err
		}
	}
	return rows.Err()
}

// quoteIdent quotes a name the way PostgreSQL prints it: bare when it is a
// plain lower-case identifier, double-quoted otherwise.
func quoteIdent(name string) string {
	plain := name != ""
	for i, r := range name {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')) {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// columnDefSQL renders a column as ALTER TABLE ... ADD COLUMN expects it,
// from what the catalog says.
func (c *colSnap) columnDefSQL() string {
	var b strings.Builder
	b.WriteString(c.name + " " + c.typ)
	switch {
	case c.generated == "s":
		fmt.Fprintf(&b, " GENERATED ALWAYS AS (%s) STORED", c.expr)
	case c.identity == "a":
		b.WriteString(" GENERATED ALWAYS AS IDENTITY")
	case c.identity == "d":
		b.WriteString(" GENERATED BY DEFAULT AS IDENTITY")
	case c.expr != "":
		b.WriteString(" DEFAULT " + c.expr)
	}
	if c.notNull && c.identity == "" {
		b.WriteString(" NOT NULL")
	}
	return b.String()
}
