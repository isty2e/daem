package journal

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestRecordedWriterViewsKeepCompleteBindingIdentity(t *testing.T) {
	root := nativeNamespaceRoot(t, t.TempDir())
	authority, err := root.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	physical := filepath.Join(root.SelectedPath, "settings.json")
	entry := recoveryEntry{Scope: string(target.ScopeGlobal), Path: address.AggregateRoot().String(), GlobalPathBinding: &recoveryGlobalPathBinding{ResolvedPath: physical, RootProvenance: persistedNativeRoot(provenance)}}
	views := recordedPathEntries([]recoveryEntry{entry, entry})
	scope, destination, err := views[0].WriterAddress()
	if err != nil || scope != target.ScopeGlobal || destination != address.AggregateRoot() || !views[0].HasGlobalBinding() || views[0].GlobalPath() != physical {
		t.Fatalf("writer view changed its recorded address/binding: %v", err)
	}
	if views[0].BindingIdentity() != views[1].BindingIdentity() {
		t.Fatal("identical complete bindings compare differently")
	}
	if err := views[0].MatchGlobalRoot(authority); err != nil {
		t.Fatal(err)
	}
	other := nativeNamespaceRoot(t, t.TempDir())
	otherAuthority, err := other.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if err := views[0].MatchGlobalRoot(otherAuthority); err == nil {
		t.Fatal("writer binding accepted another root incarnation")
	}

	for _, change := range []string{"path", "root"} {
		changed := entry
		binding := *entry.GlobalPathBinding
		changed.GlobalPathBinding = &binding
		if change == "path" {
			binding.ResolvedPath += ".other"
		} else {
			binding.RootProvenance.ObjectFingerprint += "-other"
		}
		if views[0].BindingIdentity() == recordedPathEntries([]recoveryEntry{changed})[0].BindingIdentity() {
			t.Fatal("binding identity discarded " + change)
		}
	}
	identity := views[0].BindingIdentity()
	entry.GlobalPathBinding.ResolvedPath = "changed"
	entry.GlobalPathBinding.RootProvenance.ObjectFingerprint = "changed"
	if views[0].GlobalPath() != physical || views[0].BindingIdentity() != identity {
		t.Fatal("writer view retained a mutable DTO pointer")
	}
	if err := views[0].MatchGlobalRoot(authority); err != nil {
		t.Fatal("wire mutation changed the frozen root witness", err)
	}
	zero := recordedPathEntry{}
	if _, _, err := zero.WriterAddress(); err == nil || zero.HasGlobalBinding() || zero.GlobalPath() != "" {
		t.Fatal("zero entry supplied usable writer facts")
	}
	if err := zero.MatchGlobalRoot(rootedpath.Authority{}); err == nil {
		t.Fatal("zero entry matched a root")
	}
}

func TestRecordedRecoveryEntryFreezesConsumerAndExpectedFacts(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		entry := recoveryEntry{Target: string(target.TargetPi), ExpectedAfter: recoveryExpectedPathDTO{Existed: true}}
		want := []target.Target{target.TargetPi}
		if multiple {
			entry.Targets = []string{string(target.TargetPi), string(target.TargetCodex)}
			want = append(want, target.TargetCodex)
		}
		view := recordedPathEntryView(entry)
		if multiple {
			entry.Targets[0] = string(target.TargetCodex)
		}
		entry.ExpectedAfter.Existed = false
		got, err := view.ConsumerTargets()
		if err != nil || !slices.Equal(got, want) || !view.ExpectedExists() {
			t.Fatal("recovery view did not freeze its consumer and expected facts", err)
		}
		got[0] = target.TargetCodex
		again, err := view.ConsumerTargets()
		if err != nil || !slices.Equal(again, want) {
			t.Fatal("consumer projection leaked its slice", err)
		}
	}
	if values, err := (recordedPathEntry{}).ConsumerTargets(); err == nil || len(values) != 0 {
		t.Fatal("zero view supplied a consumer set")
	}
}

func TestRecordedBackupReferencesFreezeFactsAndRefuseInvalidSets(t *testing.T) {
	digest := string(artifact.HashFileContent([]byte("before")))
	entries := []recoveryEntry{{Before: recoveryBeforePathDTO{BackupPath: "files/000001", Kind: "file", ContentHash: digest}}}
	views := recordedPathEntries(entries)
	references, err := recordedBackupReferences(t.Context(), views)
	if err != nil {
		t.Fatal(err)
	}
	entries[0].Before.BackupPath = "files/changed"
	before := references["000001"]
	if before.Path() != "files/000001" || before.Kind() != "file" || !before.Matches("file", digest) || before.Matches("directory", digest) {
		t.Fatal("backup view changed its captured facts")
	}
	delete(references, "000001")
	again, err := recordedBackupReferences(t.Context(), views)
	if err != nil || len(again) != 1 || !again["000001"].Matches("file", digest) {
		t.Fatal("caller mutation changed backup references", err)
	}
	if (recordedOrdinaryBackup{}).Matches("", "") {
		t.Fatal("zero backup matched invented evidence")
	}
	for _, name := range []string{"files/..", "files/.", "files/", "files/nested/backup", "other/000001"} {
		bad := []recoveryEntry{{Before: recoveryBeforePathDTO{BackupPath: name}}}
		if result, err := recordedBackupReferences(t.Context(), recordedPathEntries(bad)); err == nil || result != nil {
			t.Fatal("invalid backup name returned partial references: " + name)
		}
	}
	if result, err := recordedBackupReferences(t.Context(), append(views, views...)); err == nil || result != nil {
		t.Fatal("duplicate backup returned partial references")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := recordedBackupReferences(ctx, views); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("canceled backup interpretation returned a prefix")
	}
}
