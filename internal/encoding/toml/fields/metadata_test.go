package fields

import (
	"reflect"
	"strings"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

type schemaOperand struct{ value any }

func (operand *schemaOperand) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	operand.value = value
	return err
}

func TestFieldMappingRetainsUnknownOrderAndOriginalCoordinates(t *testing.T) {
	input := []byte("\ufeff[[items]]\r\nname='one'\r\noperand={path='/one'}\r\nzeta=true\r\n'a.b'=false\r\n[[items]]\r\nname='two'\r\noperand='two'\r\n")
	var decoded struct {
		Items []struct {
			Name    string        `toml:"name"`
			Operand schemaOperand `toml:"operand"`
		} `toml:"items"`
	}
	metadata, err := DecodeAdmitted(t.Context(), input, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(metadata.Undecoded(), []tomlsyntax.Key{{"items", "zeta"}, {"items", "a.b"}}) {
		t.Fatal("field classification changed original occurrence order", metadata.Undecoded())
	}
	for index, key := range metadata.Undecoded() {
		line, column, present := tomlsyntax.KeyPosition(input, metadata, key)
		if !present || line != index+4 || column != 1 {
			t.Fatal("field classification exposed normalized coordinates", key, line, column)
		}
	}
}

func TestFieldMappingRetainsRepeatedUnknownOccurrences(t *testing.T) {
	const count = 1024
	var decoded struct {
		Items []struct {
			Name string `toml:"name"`
		} `toml:"items"`
	}
	metadata, err := DecodeAdmitted(t.Context(), []byte(strings.Repeat("[[items]]\nname='kept'\nunknown=true\n", count)), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != count || len(metadata.Undecoded()) != count {
		t.Fatal("repeated unknown fields were dropped or multiplied", len(decoded.Items), len(metadata.Undecoded()))
	}
	for _, key := range metadata.Undecoded() {
		if !reflect.DeepEqual(key, tomlsyntax.Key{"items", "unknown"}) {
			t.Fatal("field classification lost a literal path", key)
		}
	}
}
