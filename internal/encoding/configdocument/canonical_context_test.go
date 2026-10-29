package configdocument

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tailscale/hujson"

	"github.com/isty2e/daem/internal/output"
)

func TestBoundedCanonicalEncodingPreservesFrozenV1Bytes(t *testing.T) {
	for _, test := range []struct {
		syntax Syntax
		input  string
	}{
		{JSON, `{"":"\u0000/~/한글\n<>&","values":[null,true,false,-0,1.2e300,{},[],{"nested":"x"}]}`},
		{JSONC, "// keep\n{\"value\":[true,],}"},
		{JSONLineComments, "// keep\n{\"value\":{\"nested\":[]},}"},
		{TOML, "integer=-9223372036854775808\nfloat=-0.0\nnan=nan\npositive=inf\nnegative=-inf\ndate=2026-10-23\ntime=12:34:56\nlocal=2026-10-23T12:34:56\noffset=2026-10-23T12:34:56Z\narray=[{a=1}]\nempty={}\n"},
	} {
		document, err := Parse(t.Context(), []byte(test.input), test.syntax, Limits{1 << 20, 32})
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := document.Lookup(output.KeyPath{})
		if err != nil || !found {
			t.Fatal("fixture root unavailable")
		}
		want := legacyCanonicalV1(value)
		if value.Canonical() != want {
			t.Fatalf("legacy canonical wire changed for %s", test.syntax)
		}
		limits := Limits{int64(len(want)), 32}
		encoded, err := value.CanonicalContext(t.Context(), limits)
		if err != nil || encoded != want {
			t.Fatalf("bounded canonical wire changed for %s: %v", test.syntax, err)
		}
		decoded, err := DecodeValue(t.Context(), test.syntax, encoded, limits)
		if err != nil || !decoded.Equal(value) {
			t.Fatalf("bounded native round trip changed value: %v", err)
		}
		limits.MaximumBytes--
		encoded, err = value.CanonicalContext(t.Context(), limits)
		if !errors.Is(err, ErrLimitExceeded) || encoded != "" {
			t.Fatal("one-byte-short canonical budget returned a partial encoding")
		}
	}
}

func TestBoundedCanonicalRefusesDepthCancellationAndQuotedExpansion(t *testing.T) {
	value, err := newString(strings.Repeat("\x00", 1000))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := value.CanonicalContext(t.Context(), Limits{1400, 4}); !errors.Is(err, ErrLimitExceeded) || encoded != "" {
		t.Fatal("private escaping expanded beyond its independent byte budget")
	}
	array, err := newArray([]Value{{data: boolNode(true)}})
	if err != nil {
		t.Fatal(err)
	}
	object, err := newObject([]objectMember{{key: "array", value: array}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := object.CanonicalContext(t.Context(), Limits{1024, 2}); err != nil {
		t.Fatal(err)
	}
	if encoded, err := object.CanonicalContext(t.Context(), Limits{1024, 1}); !errors.Is(err, ErrLimitExceeded) || encoded != "" {
		t.Fatal("semantic depth budget was confused with private metadata depth")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if encoded, err := object.CanonicalContext(ctx, Limits{1024, 4}); !errors.Is(err, context.Canceled) || encoded != "" {
		t.Fatal("canceled canonical encoding lost its cause or returned partial data")
	}
	for _, limits := range []Limits{{}, {MaximumBytes: 1}, {MaximumDepth: 1}} {
		if encoded, err := object.CanonicalContext(t.Context(), limits); err == nil || encoded != "" {
			t.Fatal("invalid encoding budget was admitted")
		}
	}
	if encoded, err := (Value{}).CanonicalContext(t.Context(), Limits{1024, 4}); err == nil || encoded != "" {
		t.Fatal("invalid value acquired private encoding")
	}
	if encoded, err := object.CanonicalContext(nil, Limits{1024, 4}); err == nil || encoded != "" {
		t.Fatal("missing operation context was accepted")
	}
}

func TestBoundedQuotedSizerMatchesPinnedJSONProducer(t *testing.T) {
	for _, text := range []string{"", "plain", "\b\f\n\r\t\\\"\x00\x1f", "한글日本語", "<>&\u2028\u2029\u202e", strings.Repeat("\x00", 1000)} {
		want := int64(len(hujson.String(text)))
		size, err := quotedSizeWithin(t.Context(), text, want)
		if err != nil || size != want {
			t.Fatalf("quoted-size admission disagrees with actual producer: %v", err)
		}
		if _, err := quotedSizeWithin(t.Context(), text, want-1); !errors.Is(err, ErrLimitExceeded) {
			t.Fatal("quoted fragment exceeded its pre-allocation budget")
		}
	}
}

// Frozen v1 compatibility oracle; production validation never calls it.
func legacyCanonicalV1(value Value) string {
	var result strings.Builder
	result.WriteString("v1:")
	var appendValue func(Value)
	appendValue = func(value Value) {
		result.WriteByte('[')
		switch node := value.data.(type) {
		case nullNode:
			result.WriteString(`"null"`)
		case boolNode:
			result.WriteString(`"bool",`)
			result.WriteString(strconv.FormatBool(bool(node)))
		case stringNode:
			result.WriteString(`"string",`)
			result.Write(hujson.String(string(node)))
		case jsonNumberNode:
			result.WriteString(`"json-number",`)
			result.Write(hujson.String(string(node)))
		case integerNode:
			result.WriteString(`"integer",`)
			result.Write(hujson.String(strconv.FormatInt(int64(node), 10)))
		case floatNode:
			result.WriteString(`"float",`)
			result.Write(hujson.String(canonicalFloat(float64(node))))
		case temporalNode:
			result.Write(hujson.String(node.kind))
			result.WriteByte(',')
			result.Write(hujson.String(node.text))
		case arrayNode:
			result.WriteString(`"array",[`)
			for index, child := range node {
				if index > 0 {
					result.WriteByte(',')
				}
				appendValue(child)
			}
			result.WriteByte(']')
		case objectNode:
			result.WriteString(`"object",[`)
			for index, member := range node {
				if index > 0 {
					result.WriteByte(',')
				}
				result.WriteByte('[')
				result.Write(hujson.String(member.key))
				result.WriteByte(',')
				appendValue(member.value)
				result.WriteByte(']')
			}
			result.WriteByte(']')
		}
		result.WriteByte(']')
	}
	appendValue(value)
	return result.String()
}
