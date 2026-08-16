package schema

// Builder is the main entry point for schema operations.
type Builder struct {
	db     DB
	d      Dialect
	tables []string // tracks creation order for DropSchema
}

type Column struct {
	Name string
	Def  ColumnDef
}

// ColumnDef describes the SQL definition of a column.
// Default values are intentionally absent — handled by crud-depot at runtime.
type ColumnDef struct {
	Type       string
	PrimaryKey bool
	Auto       bool // SERIAL / AUTO_INCREMENT — integer auto-increment
	UUID       bool // TEXT PRIMARY KEY — secure random string, value set by application
	NotNull    bool
	Unique     bool
}

type ForeignKey struct {
	Column    string
	RefTable  string
	RefColumn string
	OnDelete  string
	OnUpdate  string
}

type Index struct {
	Table   string
	Name    string
	Columns []string
	Unique  bool
}

// tagInfo holds all parsed options from a db struct tag.
// Fields like Default, OnCreate, OnWrite are parsed but ignored by
// schema-builder — they exist so parseTag doesn't need to know
// which tool consumes which options.
type tagInfo struct {
	Name      string
	PK        bool
	Auto      bool
	UUID      bool
	NotNull   bool
	Unique    bool
	Skip      bool
	RefTable  string
	RefColumn string
	OnDelete  string
	OnUpdate  string
	Index     bool
	IndexName string
}
