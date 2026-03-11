package schema

type Builder struct {
	db DB
	d  Dialect
}

type Column struct {
	Name string
	Def  ColumnDef
}

type ColumnDef struct {
	Type       string
	PrimaryKey bool
	Auto       bool // SERIAL / AUTO_INCREMENT — integer auto-increment
	UUID       bool // gen_random_uuid() / UUID() — secure random string PK
	NotNull    bool
	Unique     bool
	Default    string
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
	Default   string
	Index     bool
	IndexName string
}
