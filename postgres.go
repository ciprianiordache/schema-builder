package schemabuilder

import "fmt"

type Postgres struct{}

func (Postgres) Column(c ColumnDef) string {

	if c.PrimaryKey && c.Auto {
		return "SERIAL PRIMARY KEY"
	}

	sql := ""

	switch c.Type {
	case "int":
		sql = "INTEGER"
	case "bigint":
		sql = "BIGINT"
	case "bool":
		sql = "BOOLEAN"
	case "float":
		sql = "DOUBLE PRECISION"
	case "string":
		sql = "TEXT"
	case "time":
		sql = "TIMESTAMP"
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

func (Postgres) SupportsAlterFK() bool {
	return true
}

func (Postgres) ForeignKey(fk ForeignKey) string {
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

func (Postgres) ForeignKeyExists(
	db DB,
	table, constraint string,
) (bool, error) {
	var exists bool

	query := `
		SELECT EXISTS (
		SELECT 1
		FROM information_schema.table_constraints
		WHERE constraint_type = 'FOREIGN KEY'
		  AND table_name = $1
		  AND constraint_name = $2
		)
	`
	err := db.QueryRow(query, table, constraint).Scan(&exists)
	return exists, err
}
