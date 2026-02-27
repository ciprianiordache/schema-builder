package schemabuilder

import "testing"

func TestSQLiteColumn(t *testing.T) {
	sql := (SQLite{}).Column(ColumnDef{
		Type:       "int",
		PrimaryKey: true,
		Auto:       true,
	})

	if sql != "INTEGER PRIMARY KEY" {
		t.Fatal(sql)
	}
}

func TestPostgresColumn(t *testing.T) {
	sql := (Postgres{}).Column(ColumnDef{
		Type: "string",
	})

	if sql != "TEXT" {
		t.Fatal(sql)
	}
}

func TestMySQLAutoIncrement(t *testing.T) {
	sql := (MySQL{}).Column(ColumnDef{
		Type:       "int",
		PrimaryKey: true,
		Auto:       true,
	})

	if sql != "INT PRIMARY KEY AUTO_INCREMENT" {
		t.Fatal(sql)
	}
}
