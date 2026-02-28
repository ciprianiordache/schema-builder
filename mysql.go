package schema

import (
	"fmt"
)

type MySQL struct{}

func (MySQL) Column(c ColumnDef) string {
	sql := ""

	switch c.Type {
	case "int":
		sql = "INT"
	case "bigint":
		sql = "BIGINT"
	case "bool":
		sql = "TINYINT(1)"
	case "float":
		sql = "DOUBLE"
	case "string":
		sql = "VARCHAR(255)"
	case "time":
		sql = "DATETIME"
	}

	if c.PrimaryKey {
		sql += " PRIMARY KEY"
	}
	if c.Auto {
		sql += " AUTO_INCREMENT"
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
func (MySQL) SupportsAlterFK() bool {
	return true
}

func (MySQL) ForeignKey(fk ForeignKey) string {
	name := fmt.Sprintf(
		"CONSTRAINT fk_%s_%s",
		fk.RefTable,
		fk.Column,
	)

	sql := fmt.Sprintf(
		"%s FOREIGN KEY (%s) REFERENCES %s(%s)",
		name,
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

func (MySQL) ForeignKeyExists(
	db DB,
	table, constraint string,
) (bool, error) {

	var count int

	query := `
	SELECT COUNT(*)
	FROM information_schema.table_constraints
	WHERE constraint_type = 'FOREIGN KEY'
	  AND table_name = ?
	  AND constraint_name = ?
	  AND table_schema = DATABASE()
	`

	err := db.QueryRow(query, table, constraint).Scan(&count)
	return count > 0, err
}
