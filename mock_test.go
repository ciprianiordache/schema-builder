package schema

import (
	"database/sql"
	"errors"
)

type mockDB struct {
	queries []string
	fail    bool
}

func (m *mockDB) Exec(query string, args ...any) (sql.Result, error) {
	if m.fail {
		return nil, errors.New("exec failed")
	}
	m.queries = append(m.queries, query)
	return nil, nil
}

func (m *mockDB) Query(query string, args ...any) (*sql.Rows, error) {
	return nil, nil
}

func (m *mockDB) QueryRow(query string, args ...any) *sql.Row {
	return &sql.Row{}
}

type postgresMock struct{}

func (postgresMock) Column(c ColumnDef) string {
	return "INTEGER"
}

func (postgresMock) SupportsAlterFK() bool {
	return true
}

func (postgresMock) ForeignKey(fk ForeignKey) string {
	return "FK_SQL"
}

// întotdeauna “nu există” ca să forțeze CREATE
func (postgresMock) ForeignKeyExists(db DB, table, constraint string) (bool, error) {
	return false, nil
}
