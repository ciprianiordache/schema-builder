package schemabuilder

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
	Auto       bool
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
