package tomlsyntax

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTypedDecodeRetainsLiteralPathsAndPositions(t *testing.T) {
	content := []byte("\ufeffversion=1\r\nitems=[{name='one', 'a.b'=true}, {name='two', other=false}]\r\n")
	var decoded struct {
		Version int64 `toml:"version"`
		Items   []struct {
			Name string `toml:"name"`
		} `toml:"items"`
	}
	metadata, err := DecodeAdmitted(t.Context(), content, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != 1 || len(decoded.Items) != 2 || decoded.Items[0].Name != "one" || decoded.Items[1].Name != "two" {
		t.Fatal("unknown fields changed the decoded owned values", decoded)
	}
	for _, key := range []Key{{"items", "a.b"}, {"items", "other"}} {
		if !metadata.IsDefined(key...) {
			t.Fatal("literal source path was lost", key)
		}
		line, column, present := KeyPosition(content, metadata, key)
		if !present || line != 2 || column <= 1 {
			t.Fatal("unknown field lost its source position", key, line, column, present)
		}
	}
	keys := metadata.Keys()
	keys[0][0] = "mutated"
	if metadata.Keys()[0][0] != "version" {
		t.Fatal("metadata exposed mutable key storage")
	}
}

func TestNativeTemporalKindsAreIndependentOfLibraryTypes(t *testing.T) {
	values, err := Native(t.Context(), []byte("offset=1979-05-27T07:32:00Z\nlocal=1979-05-27T07:32:00\ndate=1979-05-27\ntime=07:32:00\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"offset": Temporal{"offset-date-time", "1979-05-27T07:32:00Z"},
		"local":  Temporal{"local-date-time", "1979-05-27T07:32:00"},
		"date":   Temporal{"local-date", "1979-05-27"},
		"time":   Temporal{"local-time", "07:32:00"},
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("temporal facts=%#v, want %#v", values, want)
	}
}

func TestRepeatedArrayFieldsRetainOneSourceOccurrenceEach(t *testing.T) {
	const count = 1024
	content := strings.Repeat("[[items]]\nname='kept'\nunknown=true\n", count)
	var decoded struct {
		Items []struct {
			Name string `toml:"name"`
		} `toml:"items"`
	}
	metadata, err := DecodeAdmitted(t.Context(), []byte(content), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	occurrences := 0
	for _, key := range metadata.Keys() {
		if reflect.DeepEqual(key, Key{"items", "unknown"}) {
			occurrences++
		}
	}
	if len(decoded.Items) != count || occurrences != count {
		t.Fatal("repeated source paths were dropped or multiplied", len(decoded.Items), occurrences)
	}
}

func TestIndexRefusesCancelledOrMalformedSourceWithoutFacts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, input := range []struct {
		ctx     context.Context
		content string
	}{
		{ctx, "value=1"},
		{t.Context(), "value=["},
	} {
		index, err := IndexDocument(input.ctx, []byte(input.content))
		if err == nil || len(index.Expressions()) != 0 || len(index.Comments()) != 0 {
			t.Fatal("refused source exposed partial syntax facts", index, err)
		}
		if input.ctx == ctx && !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost its classification", err)
		}
	}
}

func TestFinalCancellationDoesNotPublishSyntaxFacts(t *testing.T) {
	content := []byte("# keep\nvalue=1\n")
	baseline := &cancelOnErrCallContext{cancelAt: int(^uint(0) >> 1)}
	if _, err := IndexDocument(baseline, content); err != nil {
		t.Fatal(err)
	}
	index, err := IndexDocument(&cancelOnErrCallContext{cancelAt: baseline.calls}, content)
	if !errors.Is(err, context.Canceled) || len(index.Expressions()) != 0 || len(index.Comments()) != 0 {
		t.Fatal("final cancellation published syntax facts", index, err)
	}

	baseline.calls = 0
	var decoded map[string]any
	if _, err := DecodeAdmitted(baseline, content, &decoded); err != nil {
		t.Fatal(err)
	}
	metadata, err := DecodeAdmitted(&cancelOnErrCallContext{cancelAt: baseline.calls}, content, &decoded)
	if !errors.Is(err, context.Canceled) || len(metadata.Keys()) != 0 {
		t.Fatal("final cancellation published metadata", metadata, err)
	}
}

func TestNativeTemporalEncodingPreservesKindsAndRejectsInvalidLiterals(t *testing.T) {
	input := []byte("values=[1979-05-27,07:32:00,1979-05-27T07:32:00,1979-05-27T07:32:00Z]\n")
	before, err := Native(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Native(t.Context(), encoded)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("native encoding changed temporal kinds", before, after, err)
	}
	for _, invalid := range []Temporal{{"unknown", "1979-05-27"}, {"local-date", "1979-05-27\nother=true"}} {
		if _, err := Marshal(map[string]any{"value": invalid}); err == nil {
			t.Fatal("encoder accepted invalid temporal literal", invalid.Kind)
		}
	}
}

type interpretedOperand struct{ value any }

func (operand *interpretedOperand) UnmarshalTOML(content []byte) error {
	value, err := DecodeValue(content)
	operand.value = value
	return err
}

func TestCustomDecodeKeepsOriginalSourceCoordinates(t *testing.T) {
	input := []byte("\ufeff[[items]]\r\nname='one'\r\noperand={path='/one'}\r\nzeta=true\r\n'a.b'=false\r\n[[items]]\r\nname='two'\r\noperand='two'\r\n")
	var decoded struct {
		Items []struct {
			Name    string             `toml:"name"`
			Operand interpretedOperand `toml:"operand"`
		} `toml:"items"`
	}
	metadata, err := DecodeAdmitted(t.Context(), input, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 2 || decoded.Items[0].Operand.value.(map[string]any)["path"] != "/one" || decoded.Items[1].Operand.value != "two" {
		t.Fatal("private normalization changed custom operands", decoded)
	}
	for index, key := range []Key{{"items", "zeta"}, {"items", "a.b"}} {
		line, column, present := KeyPosition(input, metadata, key)
		if !present || line != index+4 || column != 1 {
			t.Fatal("private normalization exposed generated coordinates", key, line, column)
		}
	}
}
