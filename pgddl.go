package schema

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// SQL renders the whole definition as PostgreSQL DDL, one statement per
// element, in an order that applies cleanly to an empty schema.
func (d *Definition) SQL() ([]string, error) {
	if err := d.Err(); err != nil {
		return nil, err
	}
	return d.createStatements(d.tables, nil)
}

// createStatements renders CREATE TABLE (+ its indexes) for tables, in
// order. A foreign key is written inline when its target already exists or
// was created earlier in the list; otherwise it is added with ALTER TABLE
// after every table exists.
func (d *Definition) createStatements(tables []*Table, existing map[string]bool) ([]string, error) {
	created := map[string]bool{}
	for name := range existing {
		created[name] = true
	}

	var stmts, deferred []string
	for _, t := range tables {
		inline := func(fk *ForeignKeyDef) bool { return fk.refTable == t.name || created[fk.refTable] }
		create, later, err := d.createTable(t, inline)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, create)
		stmts = append(stmts, t.indexStatements()...)
		deferred = append(deferred, later...)
		created[t.name] = true
	}
	return append(stmts, deferred...), nil
}

func (d *Definition) createTable(t *Table, inline func(*ForeignKeyDef) bool) (string, []string, error) {
	width := 0
	for _, c := range t.cols {
		width = max(width, len(c.name))
	}

	var lines, deferred []string
	for _, c := range t.cols {
		def, err := d.columnSQL(c)
		if err != nil {
			return "", nil, err
		}
		if c.ref != nil {
			if err := c.ref.validate(t.name); err != nil {
				return "", nil, err
			}
			if inline(c.ref) {
				def += " " + c.ref.referencesSQL()
			} else {
				deferred = append(deferred, fmt.Sprintf("ALTER TABLE %s ADD %s;", t.name, c.ref.constraintSQL()))
			}
		}
		lines = append(lines, fmt.Sprintf("%-*s %s", width, c.name, def))
	}

	if t.pk != nil {
		lines = append(lines, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(t.pk, ", ")))
	}
	for _, u := range t.uniques {
		lines = append(lines, fmt.Sprintf("UNIQUE (%s)", strings.Join(u, ", ")))
	}
	for _, fk := range t.fks {
		if err := fk.validate(t.name); err != nil {
			return "", nil, err
		}
		if inline(fk) {
			lines = append(lines, fk.constraintSQL())
		} else {
			deferred = append(deferred, fmt.Sprintf("ALTER TABLE %s ADD %s;", t.name, fk.constraintSQL()))
		}
	}
	for _, chk := range t.checks {
		lines = append(lines, fmt.Sprintf("CHECK (%s)", chk))
	}

	if !t.hasPrimaryKey() {
		return "", nil, fmt.Errorf("schema: %s has no primary key (tag a field primary_key or call PrimaryKey)", t.name)
	}

	stmt := fmt.Sprintf("CREATE TABLE %s (\n    %s\n);", t.name, strings.Join(lines, ",\n    "))
	return stmt, deferred, nil
}

func (t *Table) hasPrimaryKey() bool {
	if t.pk != nil {
		return true
	}
	for _, c := range t.cols {
		if c.pk {
			return true
		}
	}
	return false
}

func (t *Table) indexStatements() []string {
	out := make([]string, 0, len(t.indexes))
	for _, idx := range t.indexes {
		unique := ""
		if idx.unique {
			unique = "UNIQUE "
		}
		stmt := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, idx.name, t.name, strings.Join(idx.cols, ", "))
		if idx.where != "" {
			stmt += " WHERE " + idx.where
		}
		out = append(out, stmt+";")
	}
	return out
}

// columnSQL renders everything after the column name.
func (d *Definition) columnSQL(c *TableColumn) (string, error) {
	if c.generated != "" {
		return fmt.Sprintf("%s GENERATED ALWAYS AS (%s) STORED", c.sqlType, c.generated), nil
	}

	typ := c.sqlType
	if typ == "" {
		var err error
		if typ, err = d.pgType(c); err != nil {
			return "", err
		}
	}

	var b strings.Builder
	b.WriteString(typ)

	switch {
	case c.pk && c.auto:
		if typ != "bigint" && typ != "integer" && typ != "smallint" {
			return "", fmt.Errorf("schema: %s.%s is auto but %s is not an integer type", c.table.name, c.name, typ)
		}
		b.WriteString(" GENERATED ALWAYS AS IDENTITY PRIMARY KEY")
		return b.String(), nil
	case c.pk:
		b.WriteString(" PRIMARY KEY")
	}

	if def := d.defaultSQL(c, typ); def != "" {
		b.WriteString(" DEFAULT " + def)
	}
	if (c.notNull || c.now) && !c.pk {
		b.WriteString(" NOT NULL")
	}
	if c.unique {
		b.WriteString(" UNIQUE")
	}
	for _, chk := range c.checks {
		fmt.Fprintf(&b, " CHECK (%s)", chk)
	}
	return b.String(), nil
}

func (d *Definition) defaultSQL(c *TableColumn, typ string) string {
	switch {
	case c.sqlDef != "":
		return c.sqlDef
	case c.pk && c.uuid && typ == "uuid":
		return "gen_random_uuid()"
	case c.hasDef:
		return literal(c.goType, c.tagDef)
	case c.now:
		return "now()"
	}
	return ""
}

// literal turns a default: tag value (a Go literal, as crud-depot reads it)
// into SQL: strings are quoted, numbers and booleans are not.
func literal(t reflect.Type, raw string) string {
	if t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t != nil {
		switch t.Kind() {
		case reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return raw
		}
	}
	return "'" + strings.ReplaceAll(raw, "'", "''") + "'"
}

var (
	timeType      = reflect.TypeOf(time.Time{})
	bytesType     = reflect.TypeOf([]byte(nil))
	nullTypeNames = map[reflect.Type]string{
		reflect.TypeOf(sql.NullString{}):  "text",
		reflect.TypeOf(sql.NullInt64{}):   "bigint",
		reflect.TypeOf(sql.NullInt32{}):   "integer",
		reflect.TypeOf(sql.NullInt16{}):   "smallint",
		reflect.TypeOf(sql.NullBool{}):    "boolean",
		reflect.TypeOf(sql.NullFloat64{}): "double precision",
		reflect.TypeOf(sql.NullTime{}):    "timestamptz",
	}
)

// pgType derives a PostgreSQL type from the field's Go type.
func (d *Definition) pgType(c *TableColumn) (string, error) {
	t := c.goType
	if t == nil {
		return "", fmt.Errorf("schema: %s.%s needs an explicit Type", c.table.name, c.name)
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if name, ok := nullTypeNames[t]; ok {
		return name, nil
	}
	switch t {
	case timeType:
		return "timestamptz", nil
	case bytesType:
		return "bytea", nil
	}

	switch t.Kind() {
	case reflect.String:
		if d.opts.IDType != "" && (c.name == "id" || strings.HasSuffix(c.name, "_id")) {
			return d.opts.IDType, nil
		}
		return "text", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int8, reflect.Int16, reflect.Uint8:
		return "smallint", nil
	case reflect.Int32, reflect.Uint16:
		return "integer", nil
	case reflect.Int, reflect.Int64, reflect.Uint32, reflect.Uint, reflect.Uint64:
		return "bigint", nil
	case reflect.Float32:
		return "real", nil
	case reflect.Float64:
		return "double precision", nil
	}
	return "", fmt.Errorf("schema: %s.%s: no PostgreSQL type for %s, set one with Type", c.table.name, c.name, t)
}

func (fk *ForeignKeyDef) validate(table string) error {
	switch {
	case fk.refTable == "":
		return fmt.Errorf("schema: foreign key (%s) on %s has no References", strings.Join(fk.cols, ", "), table)
	case len(fk.refCols) != len(fk.cols):
		return fmt.Errorf("schema: foreign key (%s) on %s references %d columns of %s, want %d",
			strings.Join(fk.cols, ", "), table, len(fk.refCols), fk.refTable, len(fk.cols))
	}
	return nil
}

func (fk *ForeignKeyDef) referencesSQL() string {
	s := fmt.Sprintf("REFERENCES %s (%s)", fk.refTable, strings.Join(fk.refCols, ", "))
	if fk.onDelete != "" {
		s += " ON DELETE " + fk.onDelete
	}
	if fk.onUpdate != "" {
		s += " ON UPDATE " + fk.onUpdate
	}
	return s
}

func (fk *ForeignKeyDef) constraintSQL() string {
	return fmt.Sprintf("FOREIGN KEY (%s) %s", strings.Join(fk.cols, ", "), fk.referencesSQL())
}
