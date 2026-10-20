package tomlstrict

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
)

type canonicalTestField struct {
	Value string `toml:"value,omitempty"`
}

type canonicalTestCustom struct {
	Raw map[string]any
}

func (value *canonicalTestCustom) UnmarshalTOML(raw any) error {
	value.Raw = raw.(map[string]any)
	return nil
}

type canonicalTestText struct {
	Value string
}

func (value *canonicalTestText) UnmarshalText(raw []byte) error {
	value.Value = string(raw)
	return nil
}

func TestCanonicalKeysPreserveTypedMapsCustomLeavesAndUndecoded(t *testing.T) {
	var target struct {
		Rows    map[string]*canonicalTestField `toml:"rows"`
		Items   []*canonicalTestField          `toml:"items"`
		Custom  canonicalTestCustom            `toml:"custom"`
		Text    canonicalTestText              `toml:"text"`
		Opaque  toml.Primitive                 `toml:"opaque"`
		Ignored string                         `toml:"-"`
	}
	metadata, err := toml.Decode(`unknown = 1
items = [{ value = "first" }]
rows = { TOKEN = { value = "upper" }, Token = { value = "mixed" } }
custom = { MiXeD = "owned" }
text = "custom text"
opaque = { Unchecked = "primitive" }
`, &target)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanonicalKeys(metadata, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.Rows) != 2 || target.Rows["TOKEN"].Value != "upper" || target.Rows["Token"].Value != "mixed" {
		t.Fatalf("map keys or values changed: %#v", target.Rows)
	}
	if target.Items[0].Value != "first" || target.Custom.Raw["MiXeD"] != "owned" || target.Text.Value != "custom text" {
		t.Fatalf("typed/custom leaves changed: %#v", target)
	}
	undecoded := make(map[string]bool)
	for _, key := range metadata.Undecoded() {
		undecoded[key.String()] = true
	}
	if !undecoded["unknown"] || !undecoded["opaque.Unchecked"] {
		t.Fatalf("caller-owned undecoded paths changed: %v", undecoded)
	}
}

func TestCanonicalKeysRejectTypedValueAliasesWithoutEchoingValues(t *testing.T) {
	for _, content := range []string{
		`rows = { TOKEN = { Value = "PRIVATE_VALUE_MARKER" } }`,
		`items = [{ Value = "PRIVATE_VALUE_MARKER" }]`,
	} {
		var target struct {
			Rows  map[string]*canonicalTestField `toml:"rows"`
			Items []*canonicalTestField          `toml:"items"`
		}
		metadata, err := toml.Decode(content, &target)
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateCanonicalKeys(metadata, &target)
		var canonical *NonCanonicalKeyError
		if !errors.As(err, &canonical) || canonical.canonical != "value" {
			t.Fatalf("error=%v, want canonical value spelling", err)
		}
		if strings.Contains(err.Error(), "PRIVATE_VALUE_MARKER") {
			t.Fatal("diagnostic echoed the submitted value")
		}
		key := canonical.Key()
		key[0] = "changed"
		if canonical.Key()[0] == "changed" {
			t.Fatal("key accessor exposed the diagnostic's retained path")
		}
	}
}

func TestCanonicalKeysRefuseUnsupportedStructDefinitions(t *testing.T) {
	for _, test := range []struct {
		name   string
		target any
	}{
		{"missing tag", &struct{ Value string }{}},
		{"empty name", &struct {
			Value string `toml:",omitempty"`
		}{}},
		{"missing tag in omitted child", &struct {
			Child struct{ Value string } `toml:"child"`
		}{}},
		{"missing tag behind omitted collections", &struct {
			Rows map[string][]*struct{ Value string } `toml:"rows"`
		}{}},
		{"empty name in omitted child", &struct {
			Child struct {
				Value string `toml:",omitempty"`
			} `toml:"child"`
		}{}},
		{"anonymous field in omitted child", &struct {
			Child struct{ canonicalTestField } `toml:"child"`
		}{}},
		{"anonymous field", &struct{ canonicalTestField }{}},
		{"tagged anonymous field", &struct {
			canonicalTestField `toml:"nested"`
		}{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata, err := toml.Decode("", test.target)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateCanonicalKeys(metadata, test.target); err == nil {
				t.Fatal("unsupported target definition was accepted")
			}
		})
	}

	var ignored struct {
		canonicalTestField `toml:"-"`
		Value              string `toml:"value"`
	}
	metadata, err := toml.Decode(`value = "kept"`, &ignored)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanonicalKeys(metadata, &ignored); err != nil || ignored.Value != "kept" {
		t.Fatalf("ignored anonymous field changed decoding: value=%q err=%v", ignored.Value, err)
	}
}

type canonicalRecursiveNode struct {
	Value    string                               `toml:"value"`
	Next     *canonicalRecursiveNode              `toml:"next"`
	Children map[string][]*canonicalRecursiveNode `toml:"children"`
}

type canonicalRecursiveSlice []canonicalRecursiveSlice

func TestCanonicalKeysAcceptRecursiveTypeGraphs(t *testing.T) {
	var target canonicalRecursiveNode
	metadata, err := toml.Decode(`value = "root"
next = { value = "next" }
children = { child = [{ value = "child" }] }
`, &target)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanonicalKeys(metadata, &target); err != nil {
		t.Fatal(err)
	}

	var collections struct {
		Values canonicalRecursiveSlice `toml:"values"`
	}
	metadata, err = toml.Decode("values = []\n", &collections)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanonicalKeys(metadata, &collections); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalTypeCachePreservesConcurrentDocumentChecks(t *testing.T) {
	type root struct {
		Node canonicalRecursiveNode `toml:"node"`
	}
	const callers = 32
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	for index := range callers {
		go func() {
			defer done.Done()
			alias := index%2 != 0
			content := `node = { value = "ok" }`
			if alias {
				content = `node = { Value = "invalid" }`
			}
			var target root
			metadata, err := toml.Decode(content, &target)
			ready.Done()
			<-start
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			err = ValidateCanonicalKeys(metadata, &target)
			var canonical *NonCanonicalKeyError
			if alias != errors.As(err, &canonical) || (!alias && err != nil) {
				t.Errorf("alias=%t error=%v, document verdict was not independent", alias, err)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
}
