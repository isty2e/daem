package codexplugin

import (
	"context"
	"errors"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestCodexPluginRewritePreservesOtherEntriesAndComments(t *testing.T) {
	source := codexRewriteSource(t, "alpha@market")
	before := codexRewriteDocument(t, `# root note
model = "fast"
[plugins."alpha@market"]
enabled = false
# selected note
[plugins."beta@market"]
enabled = true
# peer note
[marketplaces.market]
source = "/catalog"
`)
	for _, test := range []struct {
		name, after string
		want        bool
	}{
		{"selected entry", `# root note
model='fast'
[plugins."alpha@market"]
enabled=true
options={mode='changed'}
[plugins."beta@market"]
enabled=true
# peer note
[marketplaces.market]
source='/catalog'
`, true},
		{"selected removal", `# root note
model='fast'
[plugins."beta@market"]
enabled=true
# peer note
[marketplaces.market]
source='/catalog'
`, true},
		{"different plugin", `# root note
model='fast'
[plugins."alpha@market"]
enabled=true
[plugins."beta@market"]
enabled=false
# peer note
[marketplaces.market]
source='/catalog'
`, false},
		{"marketplace not owned by plugin edit", `# root note
model='fast'
[plugins."alpha@market"]
enabled=true
[plugins."beta@market"]
enabled=true
# peer note
[marketplaces.market]
source='/other'
`, false},
		{"selected native key", `# root note
model='slow'
[plugins."alpha@market"]
enabled=true
[plugins."beta@market"]
enabled=true
# peer note
[marketplaces.market]
source='/catalog'
`, false},
		{"lost peer comment", `# root note
model='fast'
[plugins."alpha@market"]
enabled=true
[plugins."beta@market"]
enabled=true
[marketplaces.market]
source='/catalog'
`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			preserved, err := PreservesOutsidePluginRewrite(t.Context(), before, codexRewriteDocument(t, test.after), source)
			if err != nil || preserved != test.want {
				t.Fatalf("preserved=%t, want %t: %v", preserved, test.want, err)
			}
		})
	}
}

func TestCodexMarketplaceRewriteDoesNotPermitPluginOrOtherMarketplaceChanges(t *testing.T) {
	source := codexRewriteSource(t, "alpha@market.with.dots")
	before := codexRewriteDocument(t, `model='fast'
[plugins."alpha@market.with.dots"]
enabled=true
[plugins."beta@market.with.dots"]
enabled=true
[marketplaces."market.with.dots"]
source='/old'
[marketplaces.other]
source='/keep'
# other note
`)
	for _, test := range []struct {
		name, after string
		want        bool
	}{
		{"only selected marketplace", `model='fast'
[plugins."alpha@market.with.dots"]
enabled=true
[plugins."beta@market.with.dots"]
enabled=true
[marketplaces."market.with.dots"]
source='/new'
version=2
[marketplaces.other]
source='/keep'
# other note
`, true},
		{"sibling plugin in same marketplace", `model='fast'
[plugins."alpha@market.with.dots"]
enabled=true
[plugins."beta@market.with.dots"]
enabled=false
[marketplaces."market.with.dots"]
source='/new'
[marketplaces.other]
source='/keep'
# other note
`, false},
		{"nested dotted name is not literal name", `model='fast'
[plugins."alpha@market.with.dots"]
enabled=true
[plugins."beta@market.with.dots"]
enabled=true
[marketplaces.market.with.dots]
source='/new'
[marketplaces.other]
source='/keep'
# other note
`, false},
		{"other marketplace", `model='fast'
[plugins."alpha@market.with.dots"]
enabled=true
[plugins."beta@market.with.dots"]
enabled=true
[marketplaces."market.with.dots"]
source='/new'
[marketplaces.other]
source='/changed'
# other note
`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			preserved, err := PreservesOutsideMarketplaceRewrite(t.Context(), before, codexRewriteDocument(t, test.after), source)
			if err != nil || preserved != test.want {
				t.Fatalf("preserved=%t, want %t: %v", preserved, test.want, err)
			}
		})
	}
}

func TestCodexRewriteAdmitsOnlyNecessaryParents(t *testing.T) {
	source := codexRewriteSource(t, "alpha@market")
	absent, err := configdocument.Absent(configdocument.TOML, codexRewriteLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []struct {
		name, selected, unrelated string
		qualify                   func(context.Context, configdocument.Document, configdocument.Document, desiredextension.SourceRef) (bool, error)
	}{
		{"plugin", `[plugins."alpha@market"]
enabled=true
`, `[plugins.other]
enabled=true
`, PreservesOutsidePluginRewrite},
		{"marketplace", `[marketplaces.market]
source='/catalog'
`, `[marketplaces.other]
source='/other'
`, PreservesOutsideMarketplaceRewrite},
	} {
		t.Run(compare.name, func(t *testing.T) {
			selected := codexRewriteDocument(t, compare.selected)
			for _, pair := range [][2]configdocument.Document{{absent, selected}, {selected, absent}} {
				if preserved, err := compare.qualify(t.Context(), pair[0], pair[1], source); err != nil || !preserved {
					t.Fatal("necessary container creation/removal was refused", err)
				}
			}
			if preserved, err := compare.qualify(t.Context(), absent, codexRewriteDocument(t, compare.selected+compare.unrelated), source); err != nil || preserved {
				t.Fatal("unrelated entry was hidden as a new parent", err)
			}
			if preserved, err := compare.qualify(t.Context(), codexRewriteDocument(t, "unrelated={}\n"), selected, source); err != nil || preserved {
				t.Fatal("existing empty value was discarded", err)
			}
			if preserved, err := compare.qualify(t.Context(), configdocument.Document{}, selected, source); err == nil || preserved {
				t.Fatal("invalid zero document was treated as absence")
			}
		})
	}
	if preserved, err := PreservesOutsidePluginRewrite(t.Context(), codexRewriteDocument(t, "plugins=1\n"), codexRewriteDocument(t, "[plugins.\"alpha@market\"]\nenabled=true\n"), source); err == nil || preserved {
		t.Fatal("scalar ancestor was replaced as a necessary parent")
	}
}

func TestCodexRewriteRejectsInvalidSelectionAndCancellation(t *testing.T) {
	document := codexRewriteDocument(t, "model='fast'\n")
	hostSource, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, "native")
	if err != nil {
		t.Fatal(err)
	}
	for _, qualify := range []func(context.Context, configdocument.Document, configdocument.Document, desiredextension.SourceRef) (bool, error){PreservesOutsidePluginRewrite, PreservesOutsideMarketplaceRewrite} {
		for _, source := range []desiredextension.SourceRef{{}, hostSource} {
			if preserved, err := qualify(t.Context(), document, document, source); err == nil || preserved {
				t.Fatal("invalid or non-marketplace source gained a rewrite area")
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if preserved, err := qualify(ctx, document, document, codexRewriteSource(t, "alpha@market")); !errors.Is(err, context.Canceled) || preserved {
			t.Fatal("cancelled comparison returned usable evidence", err)
		}
	}
}

func codexRewriteSource(t *testing.T, selector string) desiredextension.SourceRef {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindMarketplace, selector)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func codexRewriteDocument(t *testing.T, content string) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), []byte(content), configdocument.TOML, codexRewriteLimits())
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func codexRewriteLimits() configdocument.Limits {
	return configdocument.Limits{MaximumBytes: MaximumConfigBytes, MaximumDepth: 64}
}
