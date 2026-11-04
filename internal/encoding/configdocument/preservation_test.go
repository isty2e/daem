package configdocument_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func subtree(t *testing.T, pointer string) configdocument.RewriteRegion {
	t.Helper()
	region, err := configdocument.SubtreeRegion(path(t, pointer))
	if err != nil {
		t.Fatal(err)
	}
	return region
}

func arrayRegion(t *testing.T, pointer string, indices ...int) configdocument.RewriteRegion {
	t.Helper()
	region, err := configdocument.JSONArrayRegion(path(t, pointer), indices...)
	if err != nil {
		t.Fatal(err)
	}
	return region
}

func TestPreservationAdmitsOnlyDeclaredSubtreeChanges(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		preserved           bool
	}{
		{"new parents", `{}`, `{"plugins":{"chosen":{"enabled":true}}}`, true},
		{"removed parents", `{"plugins":{"chosen":true}}`, `{}`, true},
		{"remaining empty parent", `{"plugins":{"chosen":true}}`, `{"plugins":{}}`, true},
		{"existing empty parent", `{"plugins":{}}`, `{}`, false},
		{"unjustified empty parent", `{}`, `{"plugins":{}}`, false},
		{"unmanaged value", `{"plugins":{"chosen":true,"other":1}}`, `{"plugins":{"chosen":false,"other":2}}`, false},
		{"format only", `{"plugins":{"chosen":true},"n":1000}`, "{\n\"n\":1e3,\"plugins\":{\"chosen\":false}}", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := parse(t, configdocument.JSON, test.before), parse(t, configdocument.JSON, test.after)
			regions := []configdocument.RewriteRegion{subtree(t, "/plugins/chosen")}
			preserved, err := before.PreservesOutside(t.Context(), after, regions, regions)
			if err != nil || preserved != test.preserved {
				t.Fatalf("preservation=%v error=%v", preserved, err)
			}
		})
	}
}

func TestPreservationRetainsUnselectedJSONArrayEntriesAndComments(t *testing.T) {
	before := parse(t, configdocument.JSONC, `{
  "packages": [/* owned */ {"source":"selected"}, /* keep */ "other", "last"],
  "native": true /* keep */
}`)
	left := []configdocument.RewriteRegion{arrayRegion(t, "/packages", 0)}
	right := []configdocument.RewriteRegion{arrayRegion(t, "/packages")}
	for _, test := range []struct {
		name, content string
		preserved     bool
	}{
		{"remove selected", `{"native":true /* keep */,"packages":[/* keep */ "other","last"]}`, true},
		{"changed unselected", `{"native":true /* keep */,"packages":[/* keep */ "changed","last"]}`, false},
		{"reordered unselected", `{"native":true /* keep */,"packages":[/* keep */ "last","other"]}`, false},
		{"lost duplicate comment", `{"native":true,"packages":[/* keep */ "other","last"]}`, false},
		{"changed native", `{"native":false /* keep */,"packages":[/* keep */ "other","last"]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := parse(t, configdocument.JSONC, test.content)
			preserved, err := before.PreservesOutside(t.Context(), after, left, right)
			if err != nil || preserved != test.preserved {
				t.Fatalf("preservation=%v error=%v", preserved, err)
			}
		})
	}
}

func TestPreservationJSONArrayInsertAndEmptyBaseline(t *testing.T) {
	for _, initial := range []string{`{}`, `{"packages":[]}`} {
		before := parse(t, configdocument.JSON, initial)
		after := parse(t, configdocument.JSON, `{"packages":["selected"]}`)
		preserved, err := before.PreservesOutside(t.Context(), after,
			[]configdocument.RewriteRegion{arrayRegion(t, "/packages")},
			[]configdocument.RewriteRegion{arrayRegion(t, "/packages", 0)})
		if err != nil || !preserved {
			t.Fatal("admitted membership insertion changed preserved data", err)
		}
	}
	before, after := parse(t, configdocument.JSON, `{"packages":[]}`), parse(t, configdocument.JSON, `{}`)
	regions := []configdocument.RewriteRegion{arrayRegion(t, "/packages")}
	if preserved, err := before.PreservesOutside(t.Context(), after, regions, regions); err != nil || preserved {
		t.Fatal("unselected existing empty array disappeared", err)
	}
}

func TestPreservationTOMLCommentsFollowNativeScope(t *testing.T) {
	before := parse(t, configdocument.TOML, `# root
[plugins."chosen.dot"] # owned header
# owned table
enabled = true # owned property
[ui]
color = true # keep
# keep
`)
	regions := []configdocument.RewriteRegion{subtree(t, "/plugins/chosen.dot")}
	for _, test := range []struct {
		name, content string
		preserved     bool
	}{
		{"owned comments removed", "# root\nui.color=true # keep\n# keep\n", true},
		{"lost root comment", "ui.color=true # keep\n# keep\n", false},
		{"lost repeated comment", "# root\nui.color=true # keep\n", false},
		{"changed unselected value", "# root\nui.color=false # keep\n# keep\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := parse(t, configdocument.TOML, test.content)
			preserved, err := before.PreservesOutside(t.Context(), after, regions, regions)
			if err != nil || preserved != test.preserved {
				t.Fatalf("preservation=%v error=%v", preserved, err)
			}
		})
	}
}

func TestPreservationKeepsEmptyContainerTypesAcrossDifferentRegions(t *testing.T) {
	before := parse(t, configdocument.JSON, `{"plugins":{}}`)
	after := parse(t, configdocument.JSON, `{"plugins":["selected"]}`)
	if preserved, err := before.PreservesOutside(t.Context(), after,
		[]configdocument.RewriteRegion{subtree(t, "/plugins/chosen")},
		[]configdocument.RewriteRegion{arrayRegion(t, "/plugins", 0)}); err != nil || preserved {
		t.Fatal("owned descendant hid a changed empty-container type", err)
	}
	before, after = parse(t, configdocument.JSON, `{"plugins":[]}`), parse(t, configdocument.JSON, `{"plugins":{"chosen":true}}`)
	if preserved, err := before.PreservesOutside(t.Context(), after,
		[]configdocument.RewriteRegion{arrayRegion(t, "/plugins")},
		[]configdocument.RewriteRegion{subtree(t, "/plugins/chosen")}); err != nil || preserved {
		t.Fatal("owned property hid a changed empty-array type", err)
	}
}

func TestPreservationTOMLInlineAndTableFormsShareScope(t *testing.T) {
	for _, content := range []string{
		"plugins.chosen={enabled=true} # owned\nui.color=true # keep\n",
		"[plugins.chosen] # owned\nenabled=true\n[ui]\ncolor=true # keep\n",
		"plugins={chosen={\n# owned\nenabled=true\n}}\nui.color=true # keep\n",
	} {
		before := parse(t, configdocument.TOML, content)
		after := parse(t, configdocument.TOML, "ui.color=true # keep\n")
		regions := []configdocument.RewriteRegion{subtree(t, "/plugins/chosen")}
		preserved, err := before.PreservesOutside(t.Context(), after, regions, regions)
		if err != nil || !preserved {
			t.Fatal("equivalent TOML declaration shape changed preservation", err)
		}
	}
}

func TestPreservationRefusesInvalidRegionsWithoutChangingInputs(t *testing.T) {
	indices := []int{1, 0, 1}
	region := arrayRegion(t, "/packages", indices...)
	if !slices.Equal(indices, []int{1, 0, 1}) {
		t.Fatal("region construction mutated input")
	}
	before := parse(t, configdocument.JSON, `{"packages":["a","b"]}`)
	if preserved, err := before.PreservesOutside(t.Context(), before,
		[]configdocument.RewriteRegion{region, subtree(t, "/packages")},
		[]configdocument.RewriteRegion{subtree(t, "/packages")}); err != nil || !preserved {
		t.Fatal("overlapping regions were interpreted twice", err)
	}
	for _, regions := range [][]configdocument.RewriteRegion{
		{{}}, {arrayRegion(t, "/packages", 2)}, {subtree(t, "/packages/child")},
	} {
		if _, err := before.PreservesOutside(t.Context(), before, regions, nil); err == nil {
			t.Fatal("invalid region was accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := before.PreservesOutside(ctx, before, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled comparison succeeded", err)
	}
	if _, err := before.PreservesOutside(nil, before, nil, nil); err == nil {
		t.Fatal("nil context was accepted")
	}
	if _, err := before.PreservesOutside(t.Context(), configdocument.Document{}, nil, nil); err == nil {
		t.Fatal("uninitialized document was accepted")
	}
	if _, err := configdocument.JSONArrayRegion(path(t, "/packages"), -1); err == nil {
		t.Fatal("negative array index was accepted")
	}
	toml := parse(t, configdocument.TOML, "packages=['selected']\n")
	if _, err := toml.PreservesOutside(t.Context(), toml, []configdocument.RewriteRegion{arrayRegion(t, "/packages", 0)}, nil); err == nil {
		t.Fatal("TOML atomic array was treated as element-addressed")
	}
	if preserved, err := toml.PreservesOutside(t.Context(), parse(t, configdocument.TOML, "packages=['replacement']\n"),
		[]configdocument.RewriteRegion{subtree(t, "/packages")}, []configdocument.RewriteRegion{subtree(t, "/packages")}); err != nil || !preserved {
		t.Fatal("TOML atomic array replacement failed", err)
	}
}
