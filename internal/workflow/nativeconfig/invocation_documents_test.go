package nativeconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestInvocationDocumentsQualifyKeysOutsideContentAndCoverage(t *testing.T) {
	alias, err := pipackage.SettingsDocumentAddress(target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	parse := func(content string) configdocument.Document {
		document, err := configdocument.Parse(t.Context(), []byte(content), configdocument.JSON, DocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	original := parse(`{"theme":"wanted","foreign":1,"packages":["npm:@acme/keep@1"]}`)
	path, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := original.Lookup(path)
	if err != nil {
		t.Fatal(err)
	}
	documents := InvocationDocuments{keys: []invocationKey{{document: alias, path: path, value: value}}, documents: []invocationRead{{aliases: []aggregate.DocumentAddress{alias}}}}
	before := InvocationBaseline{documents: map[aggregate.DocumentAddress]configdocument.Document{alias: original}}
	for _, test := range []struct {
		name, content string
		cause         error
	}{
		{"same native values", `{"packages":["npm:@acme/keep@1"],"foreign":1,"theme":"wanted"}`, nil},
		{"changed key", `{"theme":"other","foreign":1,"packages":["npm:@acme/keep@1"]}`, ErrInvocationKeyChanged},
		{"outside content", `{"theme":"wanted","foreign":2,"packages":["npm:@acme/keep@1"]}`, ErrInvocationOutsideChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := InvocationBaseline{documents: map[aggregate.DocumentAddress]configdocument.Document{alias: parse(test.content)}}
			if err := documents.Qualify(t.Context(), before, after, nil); !errors.Is(err, test.cause) {
				t.Fatalf("qualification error = %v, want %v", err, test.cause)
			}
		})
	}
	retiring := InvocationDocuments{documents: documents.documents}
	if err := retiring.Qualify(t.Context(), InvocationBaseline{}, InvocationBaseline{}, nil); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("empty evidence passed a selected document boundary", err)
	}
	if err := retiring.Qualify(t.Context(), before, InvocationBaseline{}, nil); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("missing post-invocation evidence passed", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := documents.Qualify(ctx, before, before, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation became usable qualification", err)
	}
}

func TestInvocationDocumentsUseOnlyTheirProfileRewriteArea(t *testing.T) {
	alias, err := pipackage.SettingsDocumentAddress(target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	other, err := pipackage.SettingsDocumentAddress(target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	parse := func(content string) configdocument.Document {
		document, err := configdocument.Parse(t.Context(), []byte(content), configdocument.JSON, DocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	old := parse(`{"packages":["npm:@acme/keep@1"],"foreign":1}`)
	updated := parse(`{"packages":["npm:@acme/keep@1","npm:@acme/tools@1"],"foreign":1}`)
	before := InvocationBaseline{documents: map[aggregate.DocumentAddress]configdocument.Document{alias: old, other: old}}
	sourceContext, err := extensiontopology.NewLocalSourceContext("/workspace/.pi", "/home")
	if err != nil {
		t.Fatal(err)
	}
	rewrite := InvocationRewrite{Aliases: []aggregate.DocumentAddress{alias}, Preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
		return pipackage.PreservesOutsidePackageRewrite(ctx, before, after, "npm:@acme/tools@1", target.ScopeProject, sourceContext)
	}}
	documents := InvocationDocuments{documents: []invocationRead{{aliases: []aggregate.DocumentAddress{alias}}, {aliases: []aggregate.DocumentAddress{other}}}}
	after := InvocationBaseline{documents: map[aggregate.DocumentAddress]configdocument.Document{alias: updated, other: old}}
	if err := documents.Qualify(t.Context(), before, after, []InvocationRewrite{rewrite}); err != nil {
		t.Fatal("exact profile rewrite was refused", err)
	}
	after.documents[other] = updated
	if err := documents.Qualify(t.Context(), before, after, []InvocationRewrite{rewrite}); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("one layer's rewrite authority leaked to another", err)
	}
}
