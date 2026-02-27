package schemabuilder

import "database/sql"

type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type Dialect interface {
	Column(def ColumnDef) string
	ForeignKey(fk ForeignKey) string
	SupportsAlterFK() bool
	ForeignKeyExists(
		db DB,
		table string,
		constraint string,
	) (bool, error)
}

type TableNamer interface {
	TableName() string
}
