package nativeconfig

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
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
			if err := documents.Qualify(t.Context(), before, after, relationhost.NativeSettingsRewrites{}); !errors.Is(err, test.cause) {
				t.Fatalf("qualification error = %v, want %v", err, test.cause)
			}
		})
	}
	retiring := InvocationDocuments{documents: documents.documents}
	if err := retiring.Qualify(t.Context(), InvocationBaseline{}, InvocationBaseline{}, relationhost.NativeSettingsRewrites{}); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("empty evidence passed a selected document boundary", err)
	}
	if err := retiring.Qualify(t.Context(), before, InvocationBaseline{}, relationhost.NativeSettingsRewrites{}); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("missing post-invocation evidence passed", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := documents.Qualify(ctx, before, before, relationhost.NativeSettingsRewrites{}); !errors.Is(err, context.Canceled) {
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
	paths, err := daempaths.Resolve(filepath.Join(t.TempDir(), "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, "npm:@acme/tools@1")
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, source)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := relationhost.NativeSettingsProfileFor(key, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	rewrite, err := profile.BindInvocationRewrites(paths, [][]aggregate.DocumentAddress{{alias}, {other}})
	if err != nil {
		t.Fatal(err)
	}
	documents := InvocationDocuments{documents: []invocationRead{{aliases: []aggregate.DocumentAddress{alias}}, {aliases: []aggregate.DocumentAddress{other}}}}
	after := InvocationBaseline{documents: map[aggregate.DocumentAddress]configdocument.Document{alias: updated, other: old}}
	if err := documents.Qualify(t.Context(), before, after, rewrite); err != nil {
		t.Fatal("exact profile rewrite was refused", err)
	}
	after.documents[other] = updated
	if err := documents.Qualify(t.Context(), before, after, rewrite); !errors.Is(err, ErrInvocationOutsideChanged) {
		t.Fatal("one layer's rewrite authority leaked to another", err)
	}
}
