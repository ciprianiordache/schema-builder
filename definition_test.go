package schema

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type shop struct {
	ID        string    `db:"id,primary_key,uuid"`
	Name      string    `db:"name,notnull"`
	Currency  string    `db:"currency,notnull,default:RON"`
	Rate      int64     `db:"rate,notnull,default:0"`
	CreatedAt time.Time `db:"created_at,oncreate"`
}

func (shop) TableName() string { return "shops" }

type item struct {
	ID     string  `db:"id,primary_key,uuid"`
	ShopID string  `db:"shop_id,notnull"`
	Serial *string `db:"serial"`
	Price  int64   `db:"price,notnull"`
}

func (item) TableName() string { return "items" }

type itemTag struct {
	ShopID string `db:"shop_id,notnull"`
	ItemID string `db:"item_id,notnull"`
	Tag    string `db:"tag,notnull"`
}

func (itemTag) TableName() string { return "item_tags" }

type event struct {
	ID     int64  `db:"id,primary_key,auto"`
	ItemID string `db:"item_id,notnull"`
}

func (event) TableName() string { return "events" }

func TestDefinitionSQL(t *testing.T) {
	def := Define(Options{IDType: "uuid"},
		Model(shop{}, func(t *Table) {
			t.Col("name").Check(`btrim(name) <> ''`)
			t.Col("currency").Type("char(3)")
		}),
		Model(item{}, func(t *Table) {
			t.Col("shop_id").References("shops", "id").OnDelete("cascade")
			t.Unique("shop_id", "id")
			t.Check("price >= 0")
			t.Index("items_serial_uq", "shop_id", "serial").Unique().Where("serial IS NOT NULL")
		}),
		Model(itemTag{}, func(t *Table) {
			t.PrimaryKey("item_id", "tag")
			t.ForeignKey("shop_id", "item_id").References("items", "shop_id", "id").OnDelete("cascade")
		}),
		Model(event{}, func(t *Table) {
			t.Generated("twice", "bigint", "id * 2")
		}),
	)
	stmts, err := def.SQL()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stmts, "\n")

	for _, want := range []string{
		"id         uuid PRIMARY KEY DEFAULT gen_random_uuid()",
		"name       text NOT NULL CHECK (btrim(name) <> '')",
		"currency   char(3) DEFAULT 'RON' NOT NULL",
		"rate       bigint DEFAULT 0 NOT NULL",
		"created_at timestamptz DEFAULT now() NOT NULL",
		"shop_id uuid NOT NULL REFERENCES shops (id) ON DELETE CASCADE",
		"serial  text,",
		"UNIQUE (shop_id, id)",
		"CHECK (price >= 0)",
		"CREATE UNIQUE INDEX items_serial_uq ON items (shop_id, serial) WHERE serial IS NOT NULL;",
		"PRIMARY KEY (item_id, tag)",
		"FOREIGN KEY (shop_id, item_id) REFERENCES items (shop_id, id) ON DELETE CASCADE",
		"id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"twice   bigint GENERATED ALWAYS AS (id * 2) STORED",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestDefinitionSQL_ForwardReferenceIsDeferred(t *testing.T) {
	// items is declared before shops, so its foreign key waits for ALTER TABLE.
	def := Define(Options{IDType: "uuid"},
		Model(item{}, func(t *Table) { t.Col("shop_id").References("shops", "id") }),
		Model(shop{}),
	)
	stmts, err := def.SQL()
	if err != nil {
		t.Fatal(err)
	}
	last := stmts[len(stmts)-1]
	if last != "ALTER TABLE items ADD FOREIGN KEY (shop_id) REFERENCES shops (id);" {
		t.Fatalf("last statement = %q", last)
	}
	if strings.Contains(stmts[0], "REFERENCES") {
		t.Fatalf("items should not reference shops inline:\n%s", stmts[0])
	}
}

func TestDefinitionErr(t *testing.T) {
	cases := map[string]*Definition{
		"unknown column":       Define(Options{}, Model(shop{}, func(t *Table) { t.Col("nope").Check("true") })),
		"unknown index column": Define(Options{}, Model(shop{}, func(t *Table) { t.Index("x", "nope") })),
		"two primary keys":     Define(Options{}, Model(shop{}, func(t *Table) { t.PrimaryKey("name") })),
		"duplicate table":      Define(Options{}, Model(shop{}), Model(shop{})),
	}
	for name, def := range cases {
		if def.Err() == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	noPK := Define(Options{}, Model(itemTag{}))
	if _, err := noPK.SQL(); err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Errorf("table without primary key: err = %v", err)
	}
	badFK := Define(Options{}, Model(itemTag{}, func(t *Table) {
		t.PrimaryKey("item_id", "tag")
		t.ForeignKey("shop_id", "item_id").References("items", "id")
	}))
	if _, err := badFK.SQL(); err == nil {
		t.Error("foreign key with mismatched column count: want an error")
	}
}

func TestLiteral(t *testing.T) {
	var s *string
	cases := []struct {
		v    any
		raw  string
		want string
	}{
		{"", "it's", "'it''s'"},
		{s, "x", "'x'"},
		{int16(0), "80", "80"},
		{false, "true", "true"},
		{1.5, "0.5", "0.5"},
	}
	for _, c := range cases {
		if got := literal(reflect.TypeOf(c.v), c.raw); got != c.want {
			t.Errorf("literal(%T, %q) = %s, want %s", c.v, c.raw, got, c.want)
		}
	}
}
