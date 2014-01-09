package fieldsn

import (
	"reflect"
	"testing"
)

func TestFieldsN(t *testing.T) {
	type T struct {
		s      string
		n      int
		result []string
	}
	tests := []T{
		{"a b", 0, nil},
		{"a", 1, []string{"a"}},
		{"a ", 1, []string{"a "}},
		{" a", 1, []string{"a"}},
		{" a ", 1, []string{"a "}},
		{" a ", 2, []string{"a"}},
		{"a b", 2, []string{"a", "b"}},
		{"a b ", 2, []string{"a", "b "}},
		{"a b c", 2, []string{"a", "b c"}},
	}
	for _, test := range tests {
		t.Logf("%#v", test)
		result := FieldsN(test.s, test.n)
		if !reflect.DeepEqual(result, test.result) {
			t.Errorf("FieldsN(%v, %v) was %v, expecting %s", test.s, test.n, result, test.result)
		}
	}
}
