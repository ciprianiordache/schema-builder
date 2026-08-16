package schema

import "testing"

type Post struct {
	ID     int `db:"id,primary_key,auto"`
	UserID int `db:"user_id,notnull,references:users(id),index"`
}

func TestCreateSchema_SQLite(t *testing.T) {
	db := &mockDB{}
	builder := New(db, SQLite{})

	err := builder.CreateSchema(Post{})
	if err != nil {
		t.Fatal(err)
	}

	if len(db.queries) == 0 {
		t.Fatal("no SQL executed")
	}
}

func TestCreateSchema_Postgres(t *testing.T) {
	db := &mockDB{}
	builder := New(db, postgresMock{})

	err := builder.CreateSchema(Post{})
	if err != nil {
		t.Fatal(err)
	}

	// ar trebui să avem 2 exec: CREATE + ALTER (FK)
	if len(db.queries) < 2 {
		t.Fatalf("expected CREATE + ALTER, got %d queries", len(db.queries))
	}
}
