package schema

import (
	"fmt"
	"reflect"
	"strings"
)

// Options tune how a Definition turns models into PostgreSQL tables.
type Options struct {
	// IDType is the SQL type of string columns named "id" or ending in
	// "_id". Empty means "text". With "uuid", a primary key tagged uuid also
	// gets DEFAULT gen_random_uuid().
	IDType string

	// Ignore lists tables that live in the database but are not described
	// by any model. Verify and Generate leave them alone. schema_migrations
	// is always ignored.
	Ignore []string

	// Extensions the tables need ("citext"). A migration that Generate
	// writes starts with CREATE EXTENSION IF NOT EXISTS for any of them the
	// database does not have yet; its down file leaves them installed.
	Extensions []string
}

// Definition is the full, PostgreSQL-only description of a schema: every
// table, built from a model's db tags plus whatever the tags cannot say
// (composite keys, CHECKs, partial indexes, generated columns).
//
// It drives Generate (write the next migration) and Verify (fail when the
// database and the models disagree). CreateSchema is unrelated and keeps
// working as before for the other dialects.
type Definition struct {
	opts   Options
	tables []*Table
}

// Define collects tables in creation order: put a table before the tables
// that reference it. A reference to a later table still works - its foreign
// key is added with ALTER TABLE once both exist.
func Define(opts Options, tables ...*Table) *Definition {
	return &Definition{opts: opts, tables: tables}
}

// Tables returns the tables in creation order.
func (d *Definition) Tables() []*Table { return d.tables }

// Err reports the first mistake in the definition: an unknown column, a
// duplicate table, a primary key declared twice.
func (d *Definition) Err() error {
	seen := map[string]bool{}
	for _, t := range d.tables {
		if t.err != nil {
			return t.err
		}
		if seen[t.name] {
			return fmt.Errorf("schema: table %q is defined twice", t.name)
		}
		seen[t.name] = true
	}
	return nil
}

func (d *Definition) ignored(table string) bool {
	if table == "schema_migrations" {
		return true
	}
	for _, name := range d.opts.Ignore {
		if name == table {
			return true
		}
	}
	return false
}

// Table is one table of a Definition. Columns come from the model's db
// tags; the methods add what a tag cannot express.
type Table struct {
	name    string
	cols    []*TableColumn
	byName  map[string]*TableColumn
	pk      []string
	uniques [][]string
	checks  []string
	fks     []*ForeignKeyDef
	indexes []*IndexDef
	err     error
}

// Model builds a table from a struct's db tags, then lets configure add
// constraints and per-column details:
//
//	schema.Model(unit.Unit{}, func(t *schema.Table) {
//	    t.Col("label_code").Check(`label_code ~ '^[A-Z0-9]{4,12}$'`)
//	    t.Unique("organization_id", "id")
//	    t.ForeignKey("organization_id", "grade_id").References("grades", "organization_id", "id")
//	    t.Index("units_status_idx", "organization_id", "status")
//	})
func Model(model any, configure ...func(t *Table)) *Table {
	t := &Table{name: resolveTableName(model), byName: map[string]*TableColumn{}}

	typ := reflect.TypeOf(model)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.err = fmt.Errorf("schema: %s is not a struct", typ)
		return t
	}

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("db")
		if tag == "" || !f.IsExported() {
			continue
		}
		info := parseTag(tag)
		if info.Skip {
			continue
		}
		c := &TableColumn{
			table:   t,
			name:    info.Name,
			goType:  f.Type,
			notNull: info.NotNull,
			unique:  info.Unique,
			pk:      info.PK,
			auto:    info.Auto,
			uuid:    info.UUID,
			tagDef:  info.Default,
			hasDef:  info.HasDefault,
			now:     info.OnCreate || info.OnWrite,
		}
		if info.RefTable != "" {
			c.ref = &ForeignKeyDef{
				cols:     []string{info.Name},
				refTable: info.RefTable,
				refCols:  []string{info.RefColumn},
				onDelete: info.OnDelete,
				onUpdate: info.OnUpdate,
			}
		}
		t.addColumn(c)
		if info.Index {
			name := info.IndexName
			if name == "" {
				name = t.name + "_" + info.Name + "_idx"
			}
			t.indexes = append(t.indexes, &IndexDef{name: name, cols: []string{info.Name}, unique: info.Unique})
		}
	}

	for _, fn := range configure {
		fn(t)
	}
	return t
}

// Name is the table name.
func (t *Table) Name() string { return t.name }

func (t *Table) addColumn(c *TableColumn) {
	if _, dup := t.byName[c.name]; dup {
		t.fail("schema: %s.%s is declared twice", t.name, c.name)
		return
	}
	t.cols = append(t.cols, c)
	t.byName[c.name] = c
}

func (t *Table) fail(format string, args ...any) {
	if t.err == nil {
		t.err = fmt.Errorf(format, args...)
	}
}

func (t *Table) checkCols(what string, cols []string) {
	if len(cols) == 0 {
		t.fail("schema: %s on %s needs at least one column", what, t.name)
	}
	for _, c := range cols {
		// Index columns may carry an order or an expression ("valued_at DESC").
		name := strings.Fields(c)[0]
		if _, ok := t.byName[name]; !ok {
			t.fail("schema: %s on %s names unknown column %q", what, t.name, name)
		}
	}
}

// Col returns the column for further configuration. An unknown name is
// reported by Definition.Err; the returned column is then a detached dummy
// so chained calls stay safe.
func (t *Table) Col(name string) *TableColumn {
	if c, ok := t.byName[name]; ok {
		return c
	}
	t.fail("schema: %s has no column %q", t.name, name)
	return &TableColumn{table: t, name: name}
}

// PrimaryKey declares a composite primary key. Do not also tag a field
// primary_key.
func (t *Table) PrimaryKey(cols ...string) *Table {
	t.checkCols("primary key", cols)
	for _, c := range t.cols {
		if c.pk {
			t.fail("schema: %s has both a primary_key tag (%s) and PrimaryKey()", t.name, c.name)
		}
	}
	if t.pk != nil {
		t.fail("schema: %s declares PrimaryKey twice", t.name)
	}
	t.pk = cols
	return t
}

// Unique adds a table-level UNIQUE constraint over cols.
func (t *Table) Unique(cols ...string) *Table {
	t.checkCols("unique", cols)
	t.uniques = append(t.uniques, cols)
	return t
}

// Check adds a table-level CHECK constraint. Postgres names a CHECK on one
// column <table>_<column>_check; the others <table>_check, <table>_check1,
// ... in the order they are declared.
func (t *Table) Check(expr string) *Table {
	t.checks = append(t.checks, expr)
	return t
}

// ForeignKey starts a (possibly composite) foreign key over cols; finish it
// with References.
func (t *Table) ForeignKey(cols ...string) *ForeignKeyDef {
	t.checkCols("foreign key", cols)
	fk := &ForeignKeyDef{cols: cols}
	t.fks = append(t.fks, fk)
	return fk
}

// Index adds a named index over cols. A column may carry an order
// ("valued_at DESC").
func (t *Table) Index(name string, cols ...string) *IndexDef {
	t.checkCols("index "+name, cols)
	idx := &IndexDef{name: name, cols: cols}
	t.indexes = append(t.indexes, idx)
	return idx
}

// Generated adds a stored generated column that has no field in the model
// (crud-depot writes every field, and Postgres refuses writes to it).
func (t *Table) Generated(name, sqlType, expr string) *Table {
	t.addColumn(&TableColumn{table: t, name: name, sqlType: sqlType, generated: expr})
	return t
}

// TableColumn is one column of a Table.
type TableColumn struct {
	table     *Table
	name      string
	goType    reflect.Type
	sqlType   string // explicit type; empty = derived from goType
	notNull   bool
	unique    bool
	pk        bool
	auto      bool
	uuid      bool
	tagDef    string // default: value from the tag, a Go literal
	hasDef    bool
	sqlDef    string // Default(): a raw SQL expression, wins over tagDef
	now       bool   // oncreate / onwrite: DEFAULT now() NOT NULL
	checks    []string
	ref       *ForeignKeyDef
	generated string
}

// Type sets the SQL type explicitly ("date", "citext", "numeric(3,2)").
func (c *TableColumn) Type(sqlType string) *TableColumn {
	c.sqlType = sqlType
	return c
}

// Default sets a raw SQL default expression ("”", "current_date").
// It wins over a default: tag, which is a Go literal crud-depot also uses.
func (c *TableColumn) Default(expr string) *TableColumn {
	c.sqlDef = expr
	return c
}

// Check adds a column-level CHECK, named <table>_<column>_check.
func (c *TableColumn) Check(expr string) *TableColumn {
	c.checks = append(c.checks, expr)
	return c
}

// References makes the column a single-column foreign key.
func (c *TableColumn) References(table, column string) *TableColumn {
	c.ref = &ForeignKeyDef{cols: []string{c.name}, refTable: table, refCols: []string{column}}
	return c
}

// OnDelete sets the ON DELETE action of the column's reference.
func (c *TableColumn) OnDelete(action string) *TableColumn {
	if c.ref == nil {
		c.table.fail("schema: %s.%s: OnDelete without References", c.table.name, c.name)
		return c
	}
	c.ref.onDelete = strings.ToUpper(action)
	return c
}

// ForeignKeyDef is a foreign key declared with Table.ForeignKey.
type ForeignKeyDef struct {
	cols     []string
	refTable string
	refCols  []string
	onDelete string
	onUpdate string
}

// References names the referenced table and columns.
func (fk *ForeignKeyDef) References(table string, cols ...string) *ForeignKeyDef {
	fk.refTable = table
	fk.refCols = cols
	return fk
}

// OnDelete sets the ON DELETE action ("cascade", "set null", "restrict").
func (fk *ForeignKeyDef) OnDelete(action string) *ForeignKeyDef {
	fk.onDelete = strings.ToUpper(action)
	return fk
}

// OnUpdate sets the ON UPDATE action.
func (fk *ForeignKeyDef) OnUpdate(action string) *ForeignKeyDef {
	fk.onUpdate = strings.ToUpper(action)
	return fk
}

// IndexDef is an index declared with Table.Index.
type IndexDef struct {
	name   string
	cols   []string
	unique bool
	where  string
}

// Unique makes the index unique.
func (i *IndexDef) Unique() *IndexDef {
	i.unique = true
	return i
}

// Where makes the index partial.
func (i *IndexDef) Where(predicate string) *IndexDef {
	i.where = predicate
	return i
}
