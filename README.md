# Schema Builder

[![Go Reference](https://pkg.go.dev/badge/github.com/ciprianiordache/schema-builder.svg)](https://pkg.go.dev/github.com/ciprianiordache/schema-builder)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A minimal, deterministic SQL schema builder for Go.

- ❌ Not an ORM
- ❌ No migrations engine
- ❌ No schema diffing
- ✅ Idempotent
- ✅ Explicit
- ✅ Database-first

## Supports: 
    
- PostgreSQL
- MySQL
- SQLite

## How to get

You can get this package via:

```bash
go get -u github.com/ciprianiordache/schema-builder
```

## Philosophy

This package is a **schema builder**, not a migration tool.

It ensures that:

- tables exist
- columns exist
- indexes exist
- foreign keys exist

It does **not**:

- modify existing columns
- drop constraints
- infer schema differences

## Usage

### Define models 

```go
type User struct {
	ID        int    `db:"id,primary_key,auto"`
	Email     string `db:"email,unique,index"`
	ProfileID int    `db:"profile_id,references:profiles(id),on_delete:cascade"`
}

func (User) TableName() string {
	return "users"
}
```

### Create schema 

Postgres:

```go
db, _ := sql.Open("postgres", dsn)

builder := schema.New(db, schema.Postgres{})
err := builder.CreateSchema(User{}, Profile{})
if err != nil {
	log.Fatal(err)
}
```

MySQL: 
```go
db, _ := sql.Open("mysql", dsn)

builder := schema.New(db, schema.MySQL{})
err := builder.CreateSchema(User{}, Profile{})
if err != nil {
	log.Fatal(err)
}
```

SQLite: 

```go
db, err := sql.Open("sqlite3", "./test.db")

builder := schema.New(db, schema.SQLite{})
err := builder.CreateSchema(User{}, Profile{})
if err != nil {
	log.Fatal(err)
}
```


## Supported tags

| Tag                        | Description                 |
| -------------------------- | --------------------------- |
| `primary_key`              | Marks column as primary key |
| `auto`                     | Auto-increment / serial     |
| `notnull`                  | NOT NULL constraint         |
| `unique`                   | UNIQUE constraint           |
| `index`                    | Create index                |
| `index:name`               | Custom index name           |
| `default:value`            | Default value               |
| `references:table(column)` | Foreign key                 |
| `on_delete:cascade`        | ON DELETE action            |
| `on_update:cascade`        | ON UPDATE action            |
| `-`                        | Ignore field                |

## Foreign Keys 
- SQLite: inline foreign keys
- PostgreSQL / MySQL: added via `ALTER TABLE`
- Foreign keys are added **only if missing**

Schema creation is fully **idempotent**.

## Table naming 
- Default: snake_case plural
- Custom: implement `TableNamer`

```go
func (User) TableName() string {
	return "users"
}
```

## Auto-increment behavior

- PostgreSQL: `SERIAL PRIMARY KEY`
- MySQL: `INT AUTO_INCREMENT PRIMARY KEY`
- SQLite: `INTEGER PRIMARY KEY`

Note:
- The `auto` tag only works with `primary_key`.
- It is ignored on non-primary columns.

## Validation

```go 
err := schema.ValidateModels(User{}, Profile{})
```

Validates:
- missing `db` tags
- invalid foreign key definitions

## Idempotency

You can safely run `CreateSchema` multiple times:

```go
_ = builder.CreateSchema(User{}, Profile{})
_ = builder.CreateSchema(User{}, Profile{}) // ✅ safe, no errors
```


---

## 4️⃣ Recommended usage / best practices

- Define all models **before calling CreateSchema**.
- Only use **simple foreign keys**, avoid complex ALTER table logic in SQLite.
- Use `ValidateModels` to catch missing tags or invalid references early.
- Treat this builder as **database-first**, not migrations-first.

---

## 5️⃣ Example output

```sql
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    email TEXT UNIQUE,
    profile_id INT,
    FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS users_email_idx ON users (email);
```

# FAQ / Gotchas

- Q: Does it drop or modify columns?
  - A: No, it only **creates missing tables, columns, indexes, and foreign keys**.

- Q: Can I use it as a full migration tool?
  - A: No, it is **database-first** and deterministic.

- Q: How does it handle custom table names?
  - A: Implement the `TableNamer` interface.

# Contributing
Contributions are welcome! Please open issues or pull requests.

# License
MIT