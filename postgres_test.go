package schema

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testDB creates a throwaway PostgreSQL database for one test and drops it
// afterwards. The server comes from SCHEMA_BUILDER_TEST_DSN (default: the
// local docker one on 5434); without a server the test is skipped.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SCHEMA_BUILDER_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable"
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		t.Skipf("no PostgreSQL at SCHEMA_BUILDER_TEST_DSN: %v", err)
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "schema_builder_test_" + hex.EncodeToString(b)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	db, err := sql.Open("pgx", strings.Replace(dsn, "/postgres?", "/"+name+"?", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	return db
}

func shopsV1() *Definition {
	return Define(Options{IDType: "uuid"},
		Model(shop{}, func(t *Table) {
			t.Col("name").Check(`btrim(name) <> ''`)
			t.Col("currency").Type("char(3)").Check(`currency ~ '^[A-Z]{3}$'`)
		}),
		Model(item{}, func(t *Table) {
			t.Col("shop_id").References("shops", "id").OnDelete("cascade")
			t.Unique("shop_id", "id")
			t.Check("price >= 0")
			t.Index("items_serial_uq", "shop_id", "serial").Unique().Where("serial IS NOT NULL")
		}),
	)
}

// v2 changes v1 in every way the diff knows: a new table with a composite
// key, a new column, a changed CHECK, a dropped index, a new index, a
// changed default and a generated column.
func shopsV2() *Definition {
	return Define(Options{IDType: "uuid"},
		Model(shop{}, func(t *Table) {
			t.Col("name").Check(`length(name) > 1`)
			t.Col("currency").Type("char(3)").Default("'EUR'").Check(`currency ~ '^[A-Z]{3}$'`)
		}),
		Model(itemV2{}, func(t *Table) {
			t.Col("shop_id").References("shops", "id").OnDelete("cascade")
			t.Unique("shop_id", "id")
			t.Check("price >= 0")
			t.Index("items_price_idx", "shop_id", "price DESC")
			t.Generated("price_with_vat", "bigint", "price * 119 / 100")
		}),
		Model(itemTag{}, func(t *Table) {
			t.PrimaryKey("item_id", "tag")
			t.ForeignKey("shop_id", "item_id").References("items", "shop_id", "id").OnDelete("cascade")
		}),
	)
}

type itemV2 struct {
	ID     string  `db:"id,primary_key,uuid"`
	ShopID string  `db:"shop_id,notnull"`
	Serial *string `db:"serial"`
	Price  int64   `db:"price,notnull"`
	Notes  string  `db:"notes,notnull,default:"`
}

func (itemV2) TableName() string { return "items" }

func TestGenerate_MigrateVerifyRollback(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	fsys := os.DirFS(dir)

	up, down, err := Generate(ctx, db, shopsV1(), dir, "init")
	if err != nil {
		t.Fatalf("generate v1: %v", err)
	}
	if filepath.Base(up) != "0001_init.up.sql" || filepath.Base(down) != "0001_init.down.sql" {
		t.Fatalf("paths = %s, %s", up, down)
	}
	if _, _, err := Generate(ctx, db, shopsV1(), dir, "again"); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("generate with no changes: err = %v, want ErrNoChanges", err)
	}

	if applied, err := Migrate(ctx, db, fsys); err != nil || len(applied) != 1 {
		t.Fatalf("migrate v1: applied=%d err=%v", len(applied), err)
	}
	if err := Verify(ctx, db, shopsV1()); err != nil {
		t.Fatalf("verify v1: %v", err)
	}

	// Rows survive the v2 migration: it alters tables, it does not rebuild them.
	mustExec(t, db, `INSERT INTO shops (name) VALUES ('Atelier')`)
	mustExec(t, db, `INSERT INTO items (shop_id, price) SELECT id, 1000 FROM shops`)

	if err := Verify(ctx, db, shopsV2()); err == nil {
		t.Fatal("verify v2 against a v1 database: want drift")
	}
	if _, _, err := Generate(ctx, db, shopsV2(), dir, "tags_and_vat"); err != nil {
		t.Fatalf("generate v2: %v", err)
	}
	if applied, err := Migrate(ctx, db, fsys); err != nil || len(applied) != 1 {
		body, _ := os.ReadFile(filepath.Join(dir, "0002_tags_and_vat.up.sql"))
		t.Fatalf("migrate v2: applied=%d err=%v\n%s", len(applied), err, body)
	}
	if err := Verify(ctx, db, shopsV2()); err != nil {
		t.Fatalf("verify v2: %v", err)
	}
	var vat int64
	if err := db.QueryRowContext(ctx, `SELECT price_with_vat FROM items`).Scan(&vat); err != nil || vat != 1190 {
		t.Fatalf("price_with_vat = %d, %v; want 1190", vat, err)
	}

	// The generated down file must take the database exactly back to v1.
	if rolled, err := Rollback(ctx, db, fsys, 1); err != nil || len(rolled) != 1 {
		body, _ := os.ReadFile(filepath.Join(dir, "0002_tags_and_vat.down.sql"))
		t.Fatalf("rollback v2: rolled=%d err=%v\n%s", len(rolled), err, body)
	}
	if err := Verify(ctx, db, shopsV1()); err != nil {
		t.Fatalf("verify after rollback: %v", err)
	}
	if rolled, err := Rollback(ctx, db, fsys, 5); err != nil || len(rolled) != 1 {
		t.Fatalf("rollback v1: rolled=%d err=%v", len(rolled), err)
	}
	var left int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations'`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d tables left after rolling everything back", left)
	}
}

type account struct {
	ID    string `db:"id,primary_key,uuid"`
	Email string `db:"email,notnull,unique"`
}

func (account) TableName() string { return "accounts" }

func TestGenerate_InstallsExtensionsFirst(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	def := Define(Options{IDType: "uuid", Extensions: []string{"citext"}},
		Model(account{}, func(t *Table) { t.Col("email").Type("citext") }))

	if err := Verify(ctx, db, def); err == nil || !strings.Contains(err.Error(), "CREATE EXTENSION IF NOT EXISTS citext;") {
		t.Fatalf("verify on an empty database: %v", err)
	}
	up, _, err := Generate(ctx, db, def, dir, "accounts")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(up)
	if i, j := strings.Index(string(body), "CREATE EXTENSION"), strings.Index(string(body), "CREATE TABLE"); i < 0 || i > j {
		t.Fatalf("extension must come before the tables:\n%s", body)
	}
	if _, err := Migrate(ctx, db, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ctx, db, def); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO accounts (email) VALUES ('Ana@Example.com')`)
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE email = 'ana@example.com'`).Scan(&n)
	if n != 1 {
		t.Fatal("email should compare case-insensitively")
	}
}

// A development database usually has the extension already; the migration
// must still create it, or a fresh database could not apply it.
func TestGenerate_ExtensionAlreadyInDatabase(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	mustExec(t, db, `CREATE EXTENSION citext`)
	def := Define(Options{IDType: "uuid", Extensions: []string{"citext"}},
		Model(account{}, func(t *Table) { t.Col("email").Type("citext") }))

	up, _, err := Generate(ctx, db, def, dir, "accounts")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(up)
	if !strings.Contains(string(body), "CREATE EXTENSION IF NOT EXISTS citext;") {
		t.Fatalf("migration does not create the extension:\n%s", body)
	}
	if _, _, err := Generate(ctx, db, def, dir, "again"); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("second generate: err = %v, want ErrNoChanges", err)
	}
}

func TestVerify_MatchesHandWrittenSQL(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// The same schema as shopsV1, written the way a person would.
	mustExec(t, db, `
		CREATE TABLE shops (
			id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
			name       text        NOT NULL CHECK (btrim(name) <> ''),
			currency   char(3)     NOT NULL DEFAULT 'RON' CHECK (currency ~ '^[A-Z]{3}$'),
			rate       bigint      NOT NULL DEFAULT 0,
			created_at timestamptz NOT NULL DEFAULT now()
		);
		CREATE TABLE items (
			id      uuid   PRIMARY KEY DEFAULT gen_random_uuid(),
			shop_id uuid   NOT NULL REFERENCES shops (id) ON DELETE CASCADE,
			serial  text,
			price   bigint NOT NULL,
			UNIQUE (shop_id, id),
			CHECK (price >= 0)
		);
		CREATE UNIQUE INDEX items_serial_uq ON items (shop_id, serial) WHERE serial IS NOT NULL;`)

	if err := Verify(ctx, db, shopsV1()); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerify_ReportsEachDifference(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	stmts, err := shopsV1().SQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		mustExec(t, db, s)
	}
	// Drift a person could introduce with a manual fix in production.
	mustExec(t, db, `
		ALTER TABLE items DROP CONSTRAINT items_price_check;
		ALTER TABLE items ALTER COLUMN serial SET NOT NULL;
		ALTER TABLE shops ADD COLUMN extra text;
		DROP INDEX items_serial_uq;
		CREATE TABLE stray (id int PRIMARY KEY);`)

	err = Verify(ctx, db, shopsV1())
	var drift *DriftError
	if !errors.As(err, &drift) {
		t.Fatalf("want *DriftError, got %v", err)
	}
	got := strings.Join(drift.Statements, "\n")
	for _, want := range []string{
		"ALTER TABLE items ADD CONSTRAINT items_price_check CHECK ((price >= 0));",
		"ALTER TABLE items ALTER COLUMN serial DROP NOT NULL;",
		"ALTER TABLE shops DROP COLUMN extra;",
		"CREATE UNIQUE INDEX items_serial_uq ON items USING btree (shop_id, serial) WHERE (serial IS NOT NULL);",
		"-- DROP TABLE stray;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	// Ignored tables are not drift.
	mustExec(t, db, `DROP TABLE stray; CREATE TABLE legacy (id int PRIMARY KEY)`)
	def := shopsV1()
	def.opts.Ignore = []string{"legacy"}
	err = Verify(ctx, db, def)
	if errors.As(err, &drift) && strings.Contains(strings.Join(drift.Statements, "\n"), "legacy") {
		t.Fatalf("ignored table reported: %v", err)
	}
}

func TestMigrate_FailedMigrationLeavesNothing(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0001_ok.up.sql", "CREATE TABLE a (id int PRIMARY KEY);")
	write("0001_ok.down.sql", "DROP TABLE a;")
	write("0002_bad.up.sql", "CREATE TABLE b (id int PRIMARY KEY); SELECT no_such_function();")
	write("0002_bad.down.sql", "DROP TABLE b;")

	applied, err := Migrate(ctx, db, os.DirFS(dir))
	if err == nil || len(applied) != 1 {
		t.Fatalf("applied=%d err=%v; want 1 applied and an error", len(applied), err)
	}
	var b sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT to_regclass('b')::text`).Scan(&b)
	if b.Valid {
		t.Fatal("table b exists: the failed migration was not rolled back")
	}
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n)
	if n != 1 {
		t.Fatalf("schema_migrations has %d rows, want 1", n)
	}
}

func TestLoadMigrations_Errors(t *testing.T) {
	cases := map[string]map[string]string{
		"missing down": {"0001_a.up.sql": "SELECT 1"},
		"bad name":     {"1-a.up.sql": "SELECT 1"},
		"two names":    {"0001_a.up.sql": "SELECT 1", "0001_b.down.sql": "SELECT 1"},
	}
	for name, files := range cases {
		dir := t.TempDir()
		for f, body := range files {
			_ = os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644)
		}
		if _, err := LoadMigrations(os.DirFS(dir)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "embed.go"), []byte("package x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "0002_b.up.sql"), []byte("SELECT 2"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "0002_b.down.sql"), []byte("SELECT 2"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "0001_a.up.sql"), []byte("SELECT 1"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "0001_a.down.sql"), []byte("SELECT 1"), 0o644)
	migs, err := LoadMigrations(os.DirFS(dir))
	if err != nil || len(migs) != 2 || migs[0].Version != 1 || migs[1].Name != "b" {
		t.Fatalf("migs=%+v err=%v", migs, err)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("%v\n%s", err, query)
	}
}
