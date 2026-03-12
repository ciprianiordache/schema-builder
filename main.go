package schema

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
)

func New(db DB, d Dialect) *Builder {
	return &Builder{db: db, d: d}
}

// CreateSchema creates tables for all provided models.
// Tables are created in the order they are provided — put models
// with no foreign key dependencies first.
func (b *Builder) CreateSchema(models ...any) error {
	type pendingFK struct {
		Table string
		FK    ForeignKey
	}

	var (
		allIndexes  []Index
		foreignKeys []pendingFK
	)

	for _, model := range models {
		if err := validateModel(model); err != nil {
			return err
		}

		table, cols, fks, indexes := parseModel(model)

		// track order for DropSchema
		b.tables = append(b.tables, table)
		allIndexes = append(allIndexes, indexes...)

		defs := make([]string, 0, len(cols)+len(fks))
		for _, col := range cols {
			defs = append(defs,
				fmt.Sprintf("%s %s", col.Name, b.d.Column(col.Def)),
			)
		}

		if !b.d.SupportsAlterFK() {
			for _, fk := range fks {
				defs = append(defs, b.d.ForeignKey(fk))
			}
		} else {
			for _, fk := range fks {
				foreignKeys = append(foreignKeys, pendingFK{Table: table, FK: fk})
			}
		}

		query := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s (%s);",
			table,
			strings.Join(defs, ", "),
		)

		if _, err := b.db.Exec(query); err != nil {
			return fmt.Errorf("schema: create table %q: %w", table, err)
		}
	}

	// Postgres / MySQL: add FKs via ALTER after all tables exist
	if b.d.SupportsAlterFK() {
		for _, item := range foreignKeys {
			name := foreignKeyName(item.FK)

			exists, err := b.d.ForeignKeyExists(b.db, item.Table, name)
			if err != nil {
				return fmt.Errorf("schema: check FK %q on %q: %w", name, item.Table, err)
			}
			if exists {
				continue
			}

			query := fmt.Sprintf(
				"ALTER TABLE %s ADD %s;",
				item.Table,
				b.d.ForeignKey(item.FK),
			)

			if _, err := b.db.Exec(query); err != nil {
				return fmt.Errorf("schema: add FK %q on %q: %w", name, item.Table, err)
			}
		}
	}

	return b.CreateIndexes(allIndexes...)
}

// DropSchema drops all tables that were created by this Builder instance,
// in reverse order to respect foreign key constraints.
func (b *Builder) DropSchema() error {
	for i := len(b.tables) - 1; i >= 0; i-- {
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s;", b.tables[i])
		if _, err := b.db.Exec(query); err != nil {
			return fmt.Errorf("schema: drop table %q: %w", b.tables[i], err)
		}
	}
	return nil
}

// CreateIndexes creates all provided indexes, skipping existing ones.
func (b *Builder) CreateIndexes(indexes ...Index) error {
	for _, index := range indexes {
		unique := ""
		if index.Unique {
			unique = "UNIQUE "
		}

		query := fmt.Sprintf(
			"CREATE %sINDEX IF NOT EXISTS %s ON %s (%s);",
			unique,
			index.Name,
			index.Table,
			strings.Join(index.Columns, ", "),
		)

		if _, err := b.db.Exec(query); err != nil {
			return fmt.Errorf("schema: create index %q on %q: %w", index.Name, index.Table, err)
		}
	}
	return nil
}

// ValidateModels validates all models before schema creation.
// Returns an error describing the first problem found.
func ValidateModels(models ...any) error {
	for _, model := range models {
		if err := validateModel(model); err != nil {
			return err
		}
	}
	return nil
}

func validateModel(model any) error {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return fmt.Errorf("schema: %s is not a struct", t.Name())
	}

	hasPK := false

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}

		info := parseTag(tag)

		if info.PK {
			hasPK = true
			// primary key must declare how its value is generated
			if !info.Auto && !info.UUID {
				return fmt.Errorf(
					"schema: %s.%s is primary_key but missing 'auto' or 'uuid' — add one to declare how the ID is generated",
					t.Name(), f.Name,
				)
			}
		}

		if info.RefTable != "" && info.RefColumn == "" {
			return fmt.Errorf(
				"schema: %s.%s has references:%s but missing column — use references:%s(column)",
				t.Name(), f.Name, info.RefTable, info.RefTable,
			)
		}
	}

	if !hasPK {
		return fmt.Errorf("schema: %s has no primary_key field", t.Name())
	}

	return nil
}

func foreignKeyName(fk ForeignKey) string {
	return fmt.Sprintf("fk_%s_%s", fk.RefTable, fk.Column)
}

func parseModel(model any) (string, []Column, []ForeignKey, []Index) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	table := resolveTableName(model)

	var cols []Column
	var fks []ForeignKey
	var indexes []Index

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		info := parseTag(f.Tag.Get("db"))

		if info.Skip {
			continue
		}

		colDef := ColumnDef{
			Type:       goType(f.Type),
			PrimaryKey: info.PK,
			Auto:       info.Auto,
			UUID:       info.UUID,
			NotNull:    info.NotNull,
			Unique:     info.Unique,
		}

		cols = append(cols, Column{Name: info.Name, Def: colDef})

		if info.RefTable != "" {
			fks = append(fks, ForeignKey{
				Column:    info.Name,
				RefTable:  info.RefTable,
				RefColumn: info.RefColumn,
				OnDelete:  info.OnDelete,
				OnUpdate:  info.OnUpdate,
			})
		}

		if info.Index {
			name := info.IndexName
			if name == "" {
				name = table + "_" + info.Name + "_idx"
			}
			indexes = append(indexes, Index{
				Table:   table,
				Name:    name,
				Columns: []string{info.Name},
				Unique:  info.Unique,
			})
		}
	}

	return table, cols, fks, indexes
}

// parseTag reads all options from a db struct tag.
// Options unknown to schema-builder (default:, oncreate, onwrite) are
// silently ignored — they are consumed by crud-depot.
func parseTag(tag string) tagInfo {
	if tag == "-" {
		return tagInfo{Skip: true}
	}

	parts := strings.Split(tag, ",")
	info := tagInfo{Name: snake(parts[0])}

	for _, p := range parts[1:] {
		switch {
		case p == "primary_key":
			info.PK = true
		case p == "auto":
			info.Auto = true
		case p == "uuid":
			info.UUID = true
		case p == "notnull":
			info.NotNull = true
		case p == "unique":
			info.Unique = true
		case p == "index":
			info.Index = true
		case strings.HasPrefix(p, "index:"):
			info.Index = true
			info.IndexName = strings.TrimPrefix(p, "index:")
		case strings.HasPrefix(p, "references:"):
			ref := strings.Trim(strings.TrimPrefix(p, "references:"), "()")
			r := strings.Split(ref, "(")
			info.RefTable = r[0]
			if len(r) > 1 {
				info.RefColumn = strings.TrimSuffix(r[1], ")")
			}
		case strings.HasPrefix(p, "on_delete:"):
			info.OnDelete = strings.ToUpper(strings.TrimPrefix(p, "on_delete:"))
		case strings.HasPrefix(p, "on_update:"):
			info.OnUpdate = strings.ToUpper(strings.TrimPrefix(p, "on_update:"))
			// silently ignored — consumed by crud-depot:
			// default:, oncreate, onwrite
		}
	}

	return info
}

func goType(t reflect.Type) string {
	switch t {
	case reflect.TypeOf(time.Time{}):
		return "time"
	case reflect.TypeOf(sql.NullString{}):
		return "string"
	case reflect.TypeOf(sql.NullInt64{}):
		return "bigint"
	case reflect.TypeOf(sql.NullBool{}):
		return "bool"
	}

	switch t.Kind() {
	case reflect.Int, reflect.Int32:
		return "int"
	case reflect.Int64:
		return "bigint"
	case reflect.Bool:
		return "bool"
	case reflect.Float32, reflect.Float64:
		return "float"
	default:
		return "string"
	}
}

func snake(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var out []rune

	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			if unicode.IsUpper(r) &&
				(unicode.IsLower(prev) ||
					(i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				out = append(out, '_')
			}
		}
		out = append(out, unicode.ToLower(r))
	}

	return string(out)
}

func resolveTableName(model any) string {
	if tn, ok := model.(TableNamer); ok {
		return tn.TableName()
	}

	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return snake(t.Name()) + "s"
}
