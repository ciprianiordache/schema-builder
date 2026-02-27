package schemabuilder

import "testing"

type TestModel struct{}

func TestSnake(t *testing.T) {
	if snake("UserID") != "user_id" {
		t.Fatal("snake case failed")
	}
}

func TestResolveTableName(t *testing.T) {
	name := resolveTableName(TestModel{})
	if name != "test_models" {
		t.Fatalf("unexpected table name: %s", name)
	}
}
