package schemabuilder

import "testing"

type User struct {
	ID   int    `db:"id,primary_key,auto"`
	Name string `db:"name,notnull,index"`
}

func TestParseModel(t *testing.T) {
	table, cols, fks, indexes := parseModel(User{})

	if table != "users" {
		t.Fatal("table name invalid")
	}

	if len(cols) != 2 {
		t.Fatal("columns count invalid")
	}

	if len(fks) != 0 {
		t.Fatal("unexpected foreign keys")
	}

	if len(indexes) != 1 {
		t.Fatal("index not detected")
	}
}
