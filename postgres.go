package schema

import "fmt"

type Postgres struct{}

func (Postgres) Column(c ColumnDef) string {
	if c.PrimaryKey && c.Auto {
		return "SERIAL PRIMARY KEY"
	}
	if c.PrimaryKey && c.UUID {
		return "TEXT PRIMARY KEY"
	}

	var sql string
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

	return sql
}

func (Postgres) SupportsAlterFK() bool { return true }

func (Postgres) ForeignKey(fk ForeignKey) string {
	sql := fmt.Sprintf(
		"CONSTRAINT fk_%s_%s FOREIGN KEY (%s) REFERENCES %s(%s)",
		fk.RefTable, fk.Column,
		fk.Column, fk.RefTable, fk.RefColumn,
	)
	if fk.OnDelete != "" {
		sql += " ON DELETE " + fk.OnDelete
	}
	if fk.OnUpdate != "" {
		sql += " ON UPDATE " + fk.OnUpdate
	}
	return sql
}

func (Postgres) ForeignKeyExists(db DB, table, constraint string) (bool, error) {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_type = 'FOREIGN KEY'
			  AND table_name = $1
			  AND constraint_name = $2
		)`, table, constraint,
	).Scan(&exists)
	return exists, err
}
