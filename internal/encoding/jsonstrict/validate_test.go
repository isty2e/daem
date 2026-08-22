package jsonstrict

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateRejectsAmbiguousAndUnboundedDocuments(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		depth   int
		want    string
	}{
		{name: "invalid UTF-8", content: []byte{'{', '"', 'x', '"', ':', 0xff, '}'}, depth: 4, want: "valid UTF-8"},
		{name: "duplicate root key", content: []byte(`{"x":1,"x":2}`), depth: 4, want: "duplicate object key"},
		{name: "duplicate nested key", content: []byte(`{"x":{"y":1,"y":2}}`), depth: 4, want: "duplicate object key"},
		{name: "multiple values", content: []byte(`{} []`), depth: 4, want: "multiple JSON values"},
		{name: "excessive depth", content: []byte(`{"a":{"b":{"c":1}}}`), depth: 1, want: "maximum depth"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.content, "test document", test.depth)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateAcceptsOneBoundedValue(t *testing.T) {
	if err := Validate([]byte(`{"x":[1,true,null,{"y":"z"}]}`), "test document", 4); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := Validate([]byte(`{"HostField":{"TOKEN":"value"}}`), "host document", 4); err != nil {
		t.Fatalf("general Validate rejected external key spelling: %v", err)
	}
	for _, content := range []string{
		`{"emoji":"\ud83d\ude00","key\uD83D\uDE00":"value"}`,
		`{"minimum":"\ud800\udc00","maximum":"\udbff\udfff"}`,
	} {
		if err := Validate([]byte(content), "surrogate-pair document", 4); err != nil {
			t.Fatalf("Validate rejected valid surrogate pairs: %v", err)
		}
	}
}

func TestValidateRejectsUnpairedSurrogateEscapesBeforeMaterialization(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "lone high", content: `{"value":"\ud800"}`},
		{name: "lone low", content: `{"value":"\udc00"}`},
		{name: "high followed by scalar", content: `{"value":"\ud800\u0041"}`},
		{name: "high followed by escaped text", content: `{"value":"\ud800\\uDC00"}`},
		{name: "unpaired object key", content: `{"key\udfff":true}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Validate([]byte(test.content), "test document", 4)
			if err == nil || !strings.Contains(err.Error(), "unpaired UTF-16 surrogate escape") {
				t.Fatalf("Validate error = %v, want unpaired surrogate rejection", err)
			}
		})
	}
}

func TestValidateDepthBoundaryIsInclusive(t *testing.T) {
	exact := []byte(strings.Repeat("[", 4) + "0" + strings.Repeat("]", 4))
	if err := Validate(exact, "test document", 4); err != nil {
		t.Fatalf("exact-depth value rejected: %v", err)
	}
	over := []byte(strings.Repeat("[", 5) + "0" + strings.Repeat("]", 5))
	if err := Validate(over, "test document", 4); !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("over-depth error = %v, want ErrMaximumDepthExceeded", err)
	}
}

func TestValidateClassifiesDuplicateKeysAndDepth(t *testing.T) {
	duplicateErr := Validate([]byte(`{"x":{"y":1,"y":2}}`), "test document", 4)
	if !errors.Is(duplicateErr, ErrDuplicateObjectKey) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateObjectKey", duplicateErr)
	}
	depthErr := Validate([]byte(`{"x":{"y":{"z":1}}}`), "test document", 1)
	if !errors.Is(depthErr, ErrMaximumDepthExceeded) {
		t.Fatalf("depth error = %v, want ErrMaximumDepthExceeded", depthErr)
	}
	multipleErr := Validate([]byte(`{} []`), "test document", 4)
	if !errors.Is(multipleErr, ErrMultipleValues) {
		t.Fatalf("multiple-value error = %v, want ErrMultipleValues", multipleErr)
	}
}

func TestValidateVersionedObjectRequiresOnePositiveIntegerVersion(t *testing.T) {
	version, err := ValidateVersionedObject(
		[]byte(`{"version":2,"future":true}`),
		"test document",
		4,
	)
	if err != nil || version != 2 {
		t.Fatalf("ValidateVersionedObject = (%d, %v), want (2, nil)", version, err)
	}
	version, err = ValidateVersionedObject(
		[]byte(`{"future":{"nested":[1,{"value":true}]},"version":3}`),
		"test document",
		4,
	)
	if err != nil || version != 3 {
		t.Fatalf("late ValidateVersionedObject = (%d, %v), want (3, nil)", version, err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "null document", content: `null`, want: "must be a JSON object"},
		{name: "array document", content: `[]`, want: "must be a JSON object"},
		{name: "missing version", content: `{}`, want: `field "version" is required`},
		{name: "null version", content: `{"version":null}`, want: "must be a positive integer"},
		{name: "string version", content: `{"version":"2"}`, want: "must be a positive integer"},
		{name: "fractional version", content: `{"version":2.5}`, want: "must be a positive integer"},
		{name: "zero version", content: `{"version":0}`, want: "must be a positive integer"},
		{name: "negative version", content: `{"version":-1}`, want: "must be a positive integer"},
		{name: "duplicate version", content: `{"version":1,"version":2}`, want: "duplicate object key"},
		{name: "trailing value", content: `{"version":1} {}`, want: "multiple JSON values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateVersionedObject([]byte(test.content), "test document", 4)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateVersionedObject error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateVersionedObjectRejectsNonCanonicalFieldSpellings(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "version alias", content: `{"Version":2}`},
		{name: "case-folded version duplicate", content: `{"version":1,"Version":2}`},
		{name: "top-level alias", content: `{"version":2,"CLAIMS":[]}`},
		{name: "escaped alias", content: `{"version":2,"\u0043LAIMS":[]}`},
		{name: "nested alias", content: `{"version":2,"claims":[{"Path":"legacy"}]}`},
		{name: "unicode fold alias", content: `{"version":2,"claimſ":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateVersionedObject([]byte(test.content), "test document", 4)
			if err == nil || !strings.Contains(err.Error(), "ASCII lower_snake_case") {
				t.Fatalf("ValidateVersionedObject error = %v, want canonical-field rejection", err)
			}
		})
	}
}

func TestDecodeVersionEnvelopeClassifiesLegacyCurrentAndFuture(t *testing.T) {
	tests := []struct {
		name    string
		version int
		want    VersionDisposition
	}{
		{name: "legacy", version: 1, want: VersionLegacy},
		{name: "current", version: 2, want: VersionCurrent},
		{name: "future", version: 3, want: VersionFuture},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := DecodeVersionEnvelope(
				[]byte(`{"version":`+strconv.Itoa(test.version)+`}`),
				"test document",
				4,
				2,
			)
			if err != nil {
				t.Fatalf("DecodeVersionEnvelope returned error: %v", err)
			}
			if envelope.Version != test.version || envelope.Disposition != test.want {
				t.Fatalf("DecodeVersionEnvelope = %#v, want version %d disposition %d", envelope, test.version, test.want)
			}
		})
	}
}

func TestValidateContextPreservesCancellationDuringBoundedScanning(t *testing.T) {
	content := []byte(`{"value":"` + strings.Repeat("a", cancelCheckInterval*4) + `"}`)
	ctx := &cancelAfterJSONChecksContext{cancelAt: 3}
	if err := ValidateContext(ctx, content, "test document", 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateContext error = %v, want context.Canceled", err)
	}
}

func TestValidateContextRejectsPreCanceledOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateContext(ctx, []byte(`{"value":true}`), "test document", 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateContext error = %v, want context.Canceled", err)
	}
}

type cancelAfterJSONChecksContext struct {
	calls    int
	cancelAt int
}

func (ctx *cancelAfterJSONChecksContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelAfterJSONChecksContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterJSONChecksContext) Value(any) any               { return nil }
func (ctx *cancelAfterJSONChecksContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
