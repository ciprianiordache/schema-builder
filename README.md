[![Go Reference](https://pkg.go.dev/badge/github.com/ciprianiordache/schema-builder.svg)](https://pkg.go.dev/github.com/ciprianiordache/schema-builder)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

# Schema Builder

A minimal, deterministic SQL schema builder for Go.

- ❌ Not an ORM
- ✅ Idempotent `CreateSchema` for PostgreSQL, MySQL and SQLite
- ✅ PostgreSQL migrations generated from your models — composite keys,
  CHECKs, partial indexes, generated columns — reviewed as plain SQL files
  ([PostgreSQL migrations](#postgresql-migrations))
- ✅ A drift check that fails a test when the models and the database disagree
- ✅ Explicit
- ✅ Fast — no runtime overhead

---

## Supported Dialects

- PostgreSQL
- MySQL
- SQLite

---

## Install

```bash
go get -u github.com/ciprianiordache/schema-builder
```

---

## Philosophy

This package has one job — **ensure your schema exists**.

It guarantees that:

- tables exist
- columns exist
- indexes exist
- foreign keys exist

It does **not**:

- set default values — that is [crud-depot](https://github.com/ciprianiordache/crud-depot)'s responsibility
- modify existing columns
- drop constraints
- infer schema differences

Default values (`default:`, `oncreate`, `onwrite`) are intentionally ignored by `CreateSchema`.
They are part of the shared `db` tag contract and applied by crud-depot at runtime.
A PostgreSQL `Definition` (below) also renders them as column `DEFAULT`s.

---

## Usage

### Define Models

```go
package model

import "time"

type User struct {
    ID        string    `db:"id,primary_key,uuid"`
    Name      string    `db:"name,notnull"`
    Email     string    `db:"email,notnull,unique,index"`
    Role      string    `db:"role,notnull,default:operator"`
    Active    bool      `db:"active,default:false"`
    CreatedAt time.Time `db:"created_at,oncreate"`
    UpdatedAt time.Time `db:"updated_at,onwrite"`
}

func (User) TableName() string {
    return "users"
}

type Order struct {
    ID        string    `db:"id,primary_key,uuid"`
    UserID    string    `db:"user_id,notnull,references:users(id),on_delete:cascade"`
    Total     float64   `db:"total,notnull"`
    CreatedAt time.Time `db:"created_at,oncreate"`
}

func (Order) TableName() string {
    return "orders"
}
```

> Tags like `default:`, `oncreate`, and `onwrite` are silently ignored by schema-builder.
> They are read by crud-depot at runtime.

---

### Create Schema

Postgres:

```go
db, _ := sql.Open("postgres", dsn)

builder := schema.New(db, schema.Postgres{})
if err := builder.CreateSchema(
    model.User{},
    model.Order{},
); err != nil {
    log.Fatal(err)
}
```

MySQL:

```go
db, _ := sql.Open("mysql", dsn)

builder := schema.New(db, schema.MySQL{})
if err := builder.CreateSchema(
    model.User{},
    model.Order{},
); err != nil {
    log.Fatal(err)
}
```

SQLite:

```go
db, _ := sql.Open("sqlite3", "./app.db")

builder := schema.New(db, schema.SQLite{})
if err := builder.CreateSchema(
    model.User{},
    model.Order{},
); err != nil {
    log.Fatal(err)
}
```

> Pass models in dependency order — models with no foreign keys first.

---

### Drop Schema

Drops all tables created by this builder instance, in reverse order to respect foreign key constraints.

```go
builder := schema.New(db, schema.Postgres{})
builder.CreateSchema(model.User{}, model.Order{})

// useful in tests or dev environments
defer builder.DropSchema()
```

---

## Tag Reference

Tags used by schema-builder:

| Tag                         | Description                                              |
|-----------------------------|----------------------------------------------------------|
| `primary_key`               | Marks column as primary key                              |
| `auto`                      | Integer auto-increment (`SERIAL` / `AUTO_INCREMENT`)     |
| `uuid`                      | String primary key — `TEXT PRIMARY KEY`                  |
| `notnull`                   | `NOT NULL` constraint                                    |
| `unique`                    | `UNIQUE` constraint                                      |
| `index`                     | Creates an index on this column                          |
| `index:name`                | Creates an index with a custom name                      |
| `references:table(column)`  | Foreign key to another table                             |
| `on_delete:action`          | `ON DELETE CASCADE / SET NULL / RESTRICT`                |
| `on_update:action`          | `ON UPDATE CASCADE / SET NULL / RESTRICT`                |
| `-`                         | Skip this field entirely                                 |

Tags **silently ignored** by schema-builder (consumed by crud-depot):

| Tag                    | Consumed by  |
|------------------------|--------------|
| `default:value`        | crud-depot   |
| `oncreate`             | crud-depot   |
| `onwrite`              | crud-depot   |

---

## Primary Key Behavior

Every model must have exactly one `primary_key` field, and it must declare how the ID is generated:

```go
// auto-increment integer
ID int `db:"id,primary_key,auto"`

// UUID string — value set by application/crud-depot
ID string `db:"id,primary_key,uuid"`
```

Generated SQL per dialect:

| Tag combo           | Postgres              | MySQL                          | SQLite            |
|---------------------|-----------------------|--------------------------------|-------------------|
| `primary_key,auto`  | `SERIAL PRIMARY KEY`  | `BIGINT PRIMARY KEY AUTO_INCREMENT` | `INTEGER PRIMARY KEY` |
| `primary_key,uuid`  | `TEXT PRIMARY KEY`    | `VARCHAR(36) PRIMARY KEY`      | `TEXT PRIMARY KEY` |

> A `primary_key` without `auto` or `uuid` is rejected by `ValidateModels` and `CreateSchema`.

---

## Foreign Keys

```go
type Order struct {
    UserID string `db:"user_id,notnull,references:users(id),on_delete:cascade"`
}
```

- **SQLite** — foreign keys are declared inline in `CREATE TABLE`
- **Postgres / MySQL** — foreign keys are added via `ALTER TABLE` after all tables are created
- Foreign keys are added **only if they don't already exist** — safe to run multiple times

---

## Table Naming

Default: struct name converted to snake_case plural.

```
User  → users
Order → orders
MediaAccess → media_accesss  ← override this with TableName()
```

Custom: implement `TableNamer`:

```go
func (MediaAccess) TableName() string {
    return "media_access"
}
```

---

## Validation

`ValidateModels` checks all models before touching the database:

```go
if err := schema.ValidateModels(
    model.User{},
    model.Order{},
); err != nil {
    log.Fatal(err)
}
```

Validates:

- every model has exactly one `primary_key`
- `primary_key` has `auto` or `uuid`
- foreign key references include a column — `references:users(id)` not `references:users`

---

## Idempotency

`CreateSchema` is safe to call multiple times:

```go
builder.CreateSchema(model.User{}, model.Order{})
builder.CreateSchema(model.User{}, model.Order{}) // ✅ no errors, no changes
```

Uses `CREATE TABLE IF NOT EXISTS` and checks for existing foreign keys before `ALTER TABLE`.

---

## Error Handling

All errors include context about which table or constraint failed:

```go
if err := builder.CreateSchema(model.User{}, model.Order{}); err != nil {
    // "schema: create table \"orders\": ..."
    // "schema: add FK \"fk_users_user_id\" on \"orders\": ..."
    log.Fatal(err)
}
```

---

## Example SQL Output

Postgres — `CreateSchema(model.User{}, model.Order{})`:

```sql
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL,
    active BOOLEAN,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users (email);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    total DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP
);

ALTER TABLE orders ADD CONSTRAINT fk_users_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
```

> Notice: no `DEFAULT` clauses — defaults are applied by crud-depot at runtime, not by the database.

---

## PostgreSQL migrations

For an application whose data must survive schema changes, `CreateSchema`
is not enough. A `Definition` describes the full PostgreSQL schema — each
table is a model plus what a tag cannot say — and drives three tools:

| Function | What it does |
| --- | --- |
| `Generate(ctx, db, def, dir, name)` | Writes the next `NNNN_name.up.sql` / `.down.sql` into `dir`: the SQL that takes the existing migrations to the models |
| `Verify(ctx, db, def)` | Returns a `*DriftError` listing every difference between the database and the models; put it in a test |
| `Migrate` / `Rollback` | Apply or undo migrations: advisory lock, one transaction per file, `schema_migrations` bookkeeping |

### Describe the schema

```go
var def = schema.Define(
    schema.Options{IDType: "uuid", Extensions: []string{"citext"}},

    schema.Model(model.Organization{}, func(t *schema.Table) {
        t.Col("name").Check(`btrim(name) <> ''`)
        t.Col("currency").Type("char(3)").Check(`currency ~ '^[A-Z]{3}$'`)
    }),
    schema.Model(model.Unit{}, func(t *schema.Table) {
        t.Col("organization_id").References("organizations", "id").OnDelete("cascade")
        t.Col("notes").Default("''")
        t.Unique("organization_id", "id")
        t.ForeignKey("organization_id", "grade_id").References("grades", "organization_id", "id")
        t.Check("status NOT IN ('listed', 'sold') OR grade_id IS NOT NULL")
        t.Index("units_serial_uq", "organization_id", "serial").Unique().Where("serial IS NOT NULL")
        t.Index("units_date_idx", "organization_id", "created_at DESC")
    }),
    schema.Model(model.Intervention{}, func(t *schema.Table) {
        // no field in the model: Postgres computes it
        t.Generated("labor_cost", "bigint", "(minutes * labor_rate_per_hour + 30) / 60")
    }),
    schema.Model(model.PartUse{}, func(t *schema.Table) {
        t.PrimaryKey("intervention_id", "part_id")
    }),
)
```

Types come from Go: `string` → `text` (or `Options.IDType` for `id` and
`*_id` columns), `int16` → `smallint`, `int32` → `integer`, `int`/`int64` →
`bigint`, `bool` → `boolean`, `float64` → `double precision`, `time.Time` →
`timestamptz`, `[]byte` → `bytea`; a pointer is the same type, nullable.
`Type` overrides it. From the tags: `default:X` becomes `DEFAULT 'X'`
(unquoted for numbers and booleans), `oncreate` / `onwrite` become
`DEFAULT now() NOT NULL`, `primary_key,uuid` becomes
`DEFAULT gen_random_uuid()` when `IDType` is `uuid`, and
`primary_key,auto` becomes `GENERATED ALWAYS AS IDENTITY`.

Postgres names the constraints as it would for hand-written SQL
(`units_pkey`, `units_organization_id_id_key`, `units_status_check`,
`units_check`, `units_organization_id_grade_id_fkey`), so code that matches
on a constraint name keeps working.

### Generate a migration

```go
up, down, err := schema.Generate(ctx, db, def, "migrations", "add_warranty")
if errors.Is(err, schema.ErrNoChanges) { /* nothing to do */ }
```

`Generate` replays the existing migrations and builds the models, each in a
scratch schema inside a transaction that is always rolled back, then asks
PostgreSQL for both catalogs and writes the difference. No SQL is parsed:
Postgres has already normalised every type, default, CHECK and foreign key
into the same text on both sides. Any PostgreSQL database works; nothing in
it changes.

```sql
-- 0002_add_warranty.up.sql
ALTER TABLE sales ADD COLUMN warranty_months smallint DEFAULT 12 NOT NULL;
ALTER TABLE sales ADD CONSTRAINT sales_warranty_months_check CHECK ((warranty_months >= 0));
```

Review the files before committing them: a rename shows up as a drop plus
an add, and a table no model describes is only listed, commented out.
Functions, triggers and views are not modelled; write them by hand below
the generated statements (and drop them above, in the down file).

### Fail a test on drift

```go
func TestMigrationsMatchModels(t *testing.T) {
    db := testDB(t) // a database with every migration applied
    if err := schema.Verify(ctx, db, def); err != nil {
        t.Fatal(err) // lists the SQL the database is missing
    }
}
```

### Apply

```go
applied, err := schema.Migrate(ctx, db, migrations.FS) // embed.FS of *.sql
rolled, err := schema.Rollback(ctx, db, migrations.FS, 1)
```

Files follow golang-migrate's naming (`0001_init.up.sql` /
`0001_init.down.sql`); a version without both files is an error.

---

## Recommended Usage

- Run `ValidateModels` at startup before `CreateSchema`
- Pass models in dependency order — no foreign keys first
- Use `DropSchema` in tests to reset state between runs
- `CreateSchema` is **database-first**: fine for tests, prototypes and SQLite
  apps that start from scratch
- For data that must survive schema changes, use a PostgreSQL `Definition`
  with `Generate` / `Verify` / `Migrate`

---

## Works Best With

Schema-builder and [crud-depot](https://github.com/ciprianiordache/crud-depot) share the same `db` tag format and are designed to be used together:

```go
// startup
builder := schema.New(db, schema.Postgres{})
builder.CreateSchema(model.User{})

// runtime
depot := crud.New(db, crud.Postgres{})
id, err := depot.Create(&model.User{Name: "Ion"})
```

---

## FAQ

**Does it drop or modify columns?**
`CreateSchema` does not: it only creates missing tables, indexes and foreign keys.
`Generate` writes the `ALTER` statements into a migration file for you to review.

**Can I use it as a full migration tool?**
On PostgreSQL, yes: `Generate` writes the migrations, `Migrate` / `Rollback` apply them,
`Verify` catches drift. The files are golang-migrate compatible if you prefer that runner.

**Why are there no DEFAULT clauses in the `CreateSchema` SQL?**
Default values are a runtime concern there — handled by crud-depot when building INSERT queries.
A PostgreSQL `Definition` does render them, so raw SQL inserts get the same values.

**What happens if I forget `auto` or `uuid` on a primary key?**
`CreateSchema` and `ValidateModels` both return a clear error:
```
schema: User.ID is primary_key but missing 'auto' or 'uuid'
```

**How does it handle custom table names?**
Implement the `TableNamer` interface with a `TableName() string` method.

---

## Contributing

Contributions are welcome. Feel free to open issues or pull requests.

---

## License

MIT