package schemabuilder

import "testing"

func TestParseTag(t *testing.T) {
	tag := "UserID,primary_key,auto,notnull,unique,index:custom_idx,default:10"
	info := parseTag(tag)

	if info.Name != "user_id" {
		t.Fatalf("expected user_id got %s", info.Name)
	}
	if !info.PK || !info.Auto || !info.NotNull || !info.Unique {
		t.Fatal("flags not parsed correctly")
	}
	if !info.Index || info.IndexName != "custom_idx" {
		t.Fatal("index not parsed")
	}
	if info.Default != "10" {
		t.Fatal("default not parsed")
	}
}
