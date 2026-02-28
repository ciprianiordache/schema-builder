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
		table, cols, fks, indexes := parseModel(model)
		allIndexes = append(allIndexes, indexes...)

		defs := make([]string, 0, len(cols)+len(fks))

		for _, col := range cols {
			defs = append(defs,
				fmt.Sprintf("%s %s",
					col.Name,
					b.d.Column(col.Def),
				),
			)
		}

		// 🔹 SQLite: FK inline
		if !b.d.SupportsAlterFK() {
			for _, fk := range fks {
				defs = append(defs, b.d.ForeignKey(fk))
			}
		} else {
			for _, fk := range fks {
				foreignKeys = append(foreignKeys, pendingFK{
					Table: table,
					FK:    fk,
				})
			}
		}

		sql := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s (%s);",
			table,
			strings.Join(defs, ", "),
		)

		if _, err := b.db.Exec(sql); err != nil {
			return err
		}
	}

	// 🔹 Postgres / MySQL: FK via ALTER
	if b.d.SupportsAlterFK() {
		for _, item := range foreignKeys {
			name := foreignKeyName(item.FK)

			exists, err := b.d.ForeignKeyExists(
				b.db,
				item.Table,
				name,
			)
			if err != nil {
				return err
			}

			if exists {
				continue
			}

			sql := fmt.Sprintf(
				"ALTER TABLE %s ADD %s;",
				item.Table,
				b.d.ForeignKey(item.FK),
			)

			if _, err := b.db.Exec(sql); err != nil {
				return err
			}
		}
	}
	return b.CreateIndexes(allIndexes...)
}

func (b *Builder) CreateIndexes(indexes ...Index) error {
	for _, index := range indexes {
		unique := ""
		if index.Unique {
			unique = "UNIQUE "
		}

		sql := fmt.Sprintf(
			"CREATE %sINDEX IF NOT EXISTS %s ON %s (%s);",
			unique,
			index.Name,
			index.Table,
			strings.Join(index.Columns, ", "),
		)

		if _, err := b.db.Exec(sql); err != nil {
			return err
		}
	}
	return nil
}

func foreignKeyName(fk ForeignKey) string {
	return fmt.Sprintf(
		"fk_%s_%s",
		fk.RefTable,
		fk.Column,
	)
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

		colType := goType(f.Type)

		colDef := ColumnDef{
			Type:       colType,
			PrimaryKey: info.PK,
			Auto:       info.Auto,
			NotNull:    info.NotNull,
			Unique:     info.Unique,
			Default:    defaultSQL(info.Default, colType),
		}

		cols = append(cols, Column{
			Name: info.Name,
			Def:  colDef,
		})

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
		case p == "notnull":
			info.NotNull = true
		case p == "unique":
			info.Unique = true
		case p == "index":
			info.Index = true
		case strings.HasPrefix(p, "index:"):
			info.Index = true
			info.IndexName = strings.TrimPrefix(p, "index:")
		case strings.HasPrefix(p, "default:"):
			info.Default = strings.TrimPrefix(p, "default:")
		case strings.HasPrefix(p, "references:"):
			ref := strings.Trim(strings.TrimPrefix(p, "references:"), "()")
			r := strings.Split(ref, "(")
			info.RefTable = r[0]

			if len(r) > 1 {
				info.RefColumn = strings.TrimSuffix(r[1], ")")
			} else {
				info.RefColumn = ""
			}
		case strings.HasPrefix(p, "on_delete:"):
			info.OnDelete = strings.ToUpper(
				strings.TrimPrefix(p, "on_delete:"),
			)
		case strings.HasPrefix(p, "on_update:"):
			info.OnUpdate = strings.ToUpper(
				strings.TrimPrefix(p, "on_update:"),
			)
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

func defaultSQL(raw, typ string) string {
	if raw == "" {
		return ""
	}
	if typ == "time" && strings.EqualFold(raw, "current_timestamp") {
		return "CURRENT_TIMESTAMP"
	}
	if typ == "string" {
		return "'" + strings.ReplaceAll(raw, "'", "''") + "'"
	}
	return raw
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

			// word boundary:
			// 1) lower -> upper  (userID)
			// 2) upper -> upper + next lower (HTTPServer)
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

	if tableName, ok := model.(TableNamer); ok {
		return tableName.TableName()
	}

	table := reflect.TypeOf(model)
	if table.Kind() == reflect.Ptr {
		table = table.Elem()
	}

	return snake(table.Name()) + "s"
}

func ValidateModels(models ...any) error {
	for _, model := range models {
		t := reflect.TypeOf(model)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		if t.Kind() != reflect.Struct {
			return fmt.Errorf("%s is not a struct", t.Name())
		}

		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("db")
			if tag == "" {
				return fmt.Errorf(
					"model %s field %s missing db tag",
					t.Name(), f.Name,
				)
			}

			info := parseTag(tag)
			if info.RefTable != "" && info.RefColumn == "" {
				return fmt.Errorf(
					"invalid reference in %s.%s",
					t.Name(), f.Name,
				)
			}
		}
	}
	return nil
}
