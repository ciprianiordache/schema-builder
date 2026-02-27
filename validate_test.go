package schemabuilder

import "testing"

type InvalidModel struct {
	ID int
}

func TestValidateModels_Error(t *testing.T) {
	err := ValidateModels(InvalidModel{})
	if err == nil {
		t.Fatal("expected error")
	}
}

type ValidModel struct {
	ID int `db:"id,primary_key"`
}

func TestValidateModels_OK(t *testing.T) {
	if err := ValidateModels(ValidModel{}); err != nil {
		t.Fatal(err)
	}
}
