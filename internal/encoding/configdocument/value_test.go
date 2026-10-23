package configdocument

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestJSONNumbersRetainExactDecimalMeaning(t *testing.T) {
	for _, group := range [][]string{
		{"1", "1.0", "0.1e1", "100e-2"},
		{"100", "1e2", "10.0E+01"},
		{"0.00120", "12e-4", "120e-5"},
		{"0", "0.000e999999999999999999999999"},
		{"-0", "-0.000e-999999999999999999999999"},
		{"12345678901234567890123456789", "123456789012345678901234567890e-1"},
		{"1e99999999999999999999999999999", "10e99999999999999999999999999998"},
		{"1e-10000000000000000000000000000", "10e-10000000000000000000000000001"},
	} {
		first, err := newJSONNumber(group[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range group {
			value, err := newJSONNumber(raw)
			if err != nil || !value.Equal(first) {
				t.Errorf("equivalent decimal %q: value=%s, err=%v", raw, value.Canonical(), err)
			}
			if len(value.Canonical()) > len(raw)+32 {
				t.Errorf("expanded numeric magnitude for %q", raw)
			}
		}
	}

	for _, raw := range []string{"", "01", "+1", "1.", ".1", "NaN", "1e", "1 ", " 1", "null", "true"} {
		if _, err := newJSONNumber(raw); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("number %q: %v", raw, err)
		}
	}
	positive, _ := newJSONNumber("0")
	negative, _ := newJSONNumber("-0")
	if positive.Equal(negative) {
		t.Fatal("signed zero lost")
	}
}

func TestDecimalExponentCarryBorrowAndSign(t *testing.T) {
	for _, test := range []struct {
		input string
		delta int
		want  string
	}{
		{"+000", 0, "0"},
		{"-000", 0, "0"},
		{"-0001", 0, "-1"},
		{"999", 1, "1000"},
		{"1000", -1, "999"},
		{"-999", -1, "-1000"},
		{"-1000", 1, "-999"},
		{"1", -2, "-1"},
		{"-1", 2, "1"},
		{"100", -100, "0"},
		{"-100", 100, "0"},
	} {
		if got := addDecimalExponent(test.input, test.delta); got != test.want {
			t.Errorf("%s + %d = %s, want %s", test.input, test.delta, got, test.want)
		}
	}
}

func TestNativeValueCanonicalRoundTrip(t *testing.T) {
	text, err := newString("a\x00/~/日本語\n")
	if err != nil {
		t.Fatal(err)
	}
	date, err := newTemporal("offset-date-time", "2026-10-23t01:02:03.000z")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		syntax Syntax
		value  Value
	}{
		{JSON, Value{data: nullNode{}}},
		{JSON, Value{data: boolNode(false)}},
		{JSON, text},
		{TOML, Value{data: integerNode(math.MaxInt64)}},
		{TOML, Value{data: floatNode(math.Inf(1))}},
		{TOML, Value{data: floatNode(math.Inf(-1))}},
		{TOML, Value{data: floatNode(math.NaN())}},
		{TOML, Value{data: floatNode(math.Copysign(0, -1))}},
		{TOML, date},
	} {
		t.Run(test.value.Canonical(), func(t *testing.T) {
			encoded := test.value.Canonical()
			decoded, err := DecodeValue(context.Background(), test.syntax, encoded, Limits{1024, 4})
			if err != nil || !decoded.Equal(test.value) {
				t.Fatalf("round trip: %s, %v", decoded.Canonical(), err)
			}
		})
	}
	integer := Value{data: integerNode(1)}
	if integer.Equal(Value{data: floatNode(1)}) || (Value{}).Equal(Value{}) {
		t.Fatal("distinct native types or invalid values collapsed")
	}
}

func TestCanonicalContainersAreImmutableAndKeyOrdered(t *testing.T) {
	values := []Value{{data: boolNode(true)}}
	array, err := newArray(values)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = Value{}
	members := []objectMember{{key: "z", value: array}, {key: "", value: Value{data: boolNode(false)}}}
	object, err := newObject(members)
	if err != nil {
		t.Fatal(err)
	}
	members[0].key = "modified"
	decoded, err := DecodeValue(context.Background(), JSON, object.Canonical(), Limits{4096, 2})
	if err != nil || !decoded.Equal(object) || !array.IsArray() || !object.IsObject() {
		t.Fatalf("container round trip: %s, %v", object.Canonical(), err)
	}
	if strings.Contains(object.Canonical(), "modified") {
		t.Fatal("caller mutation changed value")
	}
	if _, err := DecodeValue(context.Background(), JSON, object.Canonical(), Limits{4096, 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("container depth: %v", err)
	}
}

func TestCanonicalValueAdmission(t *testing.T) {
	for _, encoded := range []string{
		``, `v2:["null"]`, `v1:["null",false]`, `v1:["bool",null]`,
		`v1:["string",null]`, `v1:["object",null]`, `v1:["array",null]`,
		`v1:["string","\ud800"]`, `v1:["json-number","1.00"]`,
		`v1:["object",[["a",["null"]],["a",["null"]]]]`,
		`v1:["object",[["z",["null"]],["a",["null"]]]]`,
		`v1:["array",[]] `,
	} {
		if _, err := DecodeValue(context.Background(), JSON, encoded, Limits{4096, 4}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("invalid encoding admitted: %q: %v", encoded, err)
		}
	}
	for _, test := range []struct {
		syntax Syntax
		value  string
	}{
		{TOML, `v1:["null"]`},
		{TOML, `v1:["json-number","1"]`},
		{JSON, `v1:["integer","1"]`},
		{JSONC, `v1:["local-date","2026-01-01"]`},
	} {
		if _, err := DecodeValue(context.Background(), test.syntax, test.value, Limits{4096, 4}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("cross-grammar value admitted: %s: %v", test.value, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeValue(ctx, JSON, `v1:["null"]`, Limits{4096, 4}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := DecodeValue(context.Background(), JSON, `v1:["null"]`, Limits{4, 4}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("byte limit: %v", err)
	}
}
