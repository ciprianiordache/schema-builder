package schemabuilder

import "fmt"

type SQLite struct{}

func (SQLite) Column(c ColumnDef) string {
	// SQLite auto-increment ONLY works on INTEGER PRIMARY KEY
	if c.PrimaryKey && c.Auto {
		return "INTEGER PRIMARY KEY"
	}

	sql := ""

	switch c.Type {
	case "int", "bigint":
		sql = "INTEGER"
	case "bool":
		sql = "INTEGER"
	case "float":
		sql = "REAL"
	case "string":
		sql = "TEXT"
	case "time":
		sql = "DATETIME"
	}

	if c.PrimaryKey {
		sql += " PRIMARY KEY"
	}
	if c.NotNull {
		sql += " NOT NULL"
	}
	if c.Unique {
		sql += " UNIQUE"
	}
	if c.Default != "" {
		sql += " DEFAULT " + c.Default
	}

	return sql
}

func (SQLite) SupportsAlterFK() bool {
	return false
}

func (SQLite) ForeignKey(fk ForeignKey) string {
	sql := fmt.Sprintf(
		"FOREIGN KEY (%s) REFERENCES %s(%s)",
		fk.Column,
		fk.RefTable,
		fk.RefColumn,
	)

	if fk.OnDelete != "" {
		sql += " ON DELETE " + fk.OnDelete
	}

	if fk.OnUpdate != "" {
		sql += " ON UPDATE " + fk.OnUpdate
	}

	return sql
}

func (SQLite) ForeignKeyExists(
	db DB,
	table, constraint string,
) (bool, error) {
	return false, nil
}
