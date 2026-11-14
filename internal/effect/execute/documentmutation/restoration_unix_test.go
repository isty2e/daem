//go:build darwin || linux

package documentmutation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
)

const (
	restorationBefore   = `{"model":"balanced","outside":"old","packages":["npm:a","npm:b"]}`
	restorationExpected = `{"model":"fast","outside":"old","packages":["npm:b","npm:a"]}`
)

func TestDocumentRestorationPublicationRestoresMixedUnitsAndRetries(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		changed bool
	}{
		{"native expected order before", `{"model":"fast","outside":"new","packages":["npm:a","npm:b"]}`, true},
		{"native before order expected", `{"model":"balanced","outside":"new","packages":["npm:b","npm:a"]}`, true},
		{"both expected", `{"model":"fast","outside":"new","packages":["npm:b","npm:a"]}`, true},
		{"already before", `{"model":"balanced","outside":"new","packages":["npm:a","npm:b"]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			authority, destination, path, store := restorationPublicationFixture(t, test.current)
			mode := recovery.PermissionMode(0o644)
			comparison := restorationPublicationComparison(t, mode)
			staged := aggregate.ExistingDocument([]byte(test.current))
			outcome := restoreDocumentDestination(t.Context(), authority, destination, comparison, staged, &mode)
			if outcome.err != nil || outcome.commitAttempted != test.changed {
				t.Fatalf("mixed publication: attempted=%v, error=%v", outcome.commitAttempted, outcome.err)
			}
			wantWrites := 0
			if test.changed {
				wantWrites = 1
			}
			if store.writes != wantWrites {
				t.Fatalf("mutator calls=%d, want %d", store.writes, wantWrites)
			}
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertRestorationPublicationProjection(t, actual, []byte(test.current))
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("existing mode changed: %v", err)
			}
			outcome = restoreDocumentDestination(t.Context(), authority, destination, comparison, aggregate.ExistingDocument(actual), &mode)
			if outcome.err != nil || outcome.commitAttempted || store.writes != wantWrites {
				t.Fatalf("retry was not a no-write result: %v", outcome.err)
			}
			assertMutationTestFile(t, path, string(actual))
		})
	}
}

func TestDocumentRestorationPublicationRejectsStagedDriftAndThirdStates(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		staged  string
		mode    fs.FileMode
	}{
		{"staged bytes", `{"model":"fast","outside":"new","packages":["npm:b","npm:a"]}`, restorationExpected, 0o644},
		{"staged mode", restorationExpected, restorationExpected, 0o600},
		{"third native", `{"model":"third","packages":["npm:b","npm:a"]}`, `{"model":"third","packages":["npm:b","npm:a"]}`, 0o644},
		{"third Pi", `{"model":"fast","packages":["npm:c","npm:a"]}`, `{"model":"fast","packages":["npm:c","npm:a"]}`, 0o644},
	} {
		t.Run(test.name, func(t *testing.T) {
			authority, destination, path, store := restorationPublicationFixture(t, test.current)
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			mode := recovery.PermissionMode(0o644)
			outcome := restoreDocumentDestination(t.Context(), authority, destination, restorationPublicationComparison(t, mode), aggregate.ExistingDocument([]byte(test.staged)), &mode)
			if outcome.err == nil || outcome.commitAttempted || store.writes != 0 {
				t.Fatalf("interference reached a mutator: %+v, writes=%d", outcome, store.writes)
			}
			assertMutationTestFile(t, path, test.current)
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != test.mode {
				t.Fatalf("refusal altered mode: %v", err)
			}
		})
	}
}

func TestDocumentRestorationPublicationRechecksBeforeWriting(t *testing.T) {
	for _, change := range []string{"bytes", "mode", "identity"} {
		t.Run(change, func(t *testing.T) {
			authority, destination, path, store := restorationPublicationFixture(t, restorationExpected)
			current := restorationExpected
			store.beforeRead = func(count int) {
				if count != 2 {
					return
				}
				switch change {
				case "bytes":
					current = `{"model":"fast","outside":"new","packages":["npm:b","npm:a"]}`
					if err := os.WriteFile(path, []byte(current), 0o644); err != nil {
						t.Fatal(err)
					}
				case "mode":
					if err := os.Chmod(path, 0o600); err != nil {
						t.Fatal(err)
					}
				case "identity":
					if err := os.WriteFile(path+".replacement", []byte(current), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(path+".replacement", path); err != nil {
						t.Fatal(err)
					}
				}
			}
			mode := recovery.PermissionMode(0o644)
			outcome := restoreDocumentDestination(t.Context(), authority, destination, restorationPublicationComparison(t, mode), aggregate.ExistingDocument([]byte(restorationExpected)), &mode)
			if outcome.err == nil || outcome.commitAttempted || store.reads != 2 || store.writes != 0 {
				t.Fatalf("last-read drift reached a mutator: %+v, reads=%d writes=%d", outcome, store.reads, store.writes)
			}
			assertMutationTestFile(t, path, current)
		})
	}
}

func TestDocumentRestorationPublicationRejectsAncestorRedirect(t *testing.T) {
	authority, destination, path, store := restorationPublicationFixture(t, restorationExpected)
	parent, outside := filepath.Dir(path), t.TempDir()
	outsideFile := filepath.Join(outside, filepath.Base(path))
	writeMutationTestFile(t, outsideFile, "outside")
	store.beforeRead = func(count int) {
		if count != 2 {
			return
		}
		if err := os.Rename(parent, parent+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, parent); err != nil {
			t.Fatal(err)
		}
	}
	mode := recovery.PermissionMode(0o644)
	outcome := restoreDocumentDestination(t.Context(), authority, destination, restorationPublicationComparison(t, mode), aggregate.ExistingDocument([]byte(restorationExpected)), &mode)
	if outcome.err == nil || outcome.commitAttempted || store.writes != 0 {
		t.Fatalf("redirect reached a mutator: %+v", outcome)
	}
	assertMutationTestFile(t, outsideFile, "outside")
	assertMutationTestFile(t, filepath.Join(parent+"-moved", filepath.Base(path)), restorationExpected)
}

func TestDocumentRestorationPublicationChecksContextBeforeIO(t *testing.T) {
	authority, destination, _, store := restorationPublicationFixture(t, restorationExpected)
	mode := recovery.PermissionMode(0o644)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		outcome := restoreDocumentDestination(ctx, authority, destination, restorationPublicationComparison(t, mode), aggregate.ExistingDocument([]byte(restorationExpected)), &mode)
		if outcome.err == nil || outcome.commitAttempted || store.reads != 0 || store.writes != 0 {
			t.Fatalf("invalid context reached I/O: %+v", outcome)
		}
	}
}

func TestDocumentRestorationPublicationRecreatesRecordedRoot(t *testing.T) {
	authority, destination, path, store := restorationPublicationFixture(t, "remove before observation")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o644)
	before := aggregate.ExistingDocument([]byte("opaque original"))
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{
		Before: before, BeforeMode: &mode, Expected: aggregate.AbsentDocument(),
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome := restoreDocumentDestination(t.Context(), authority, destination, comparison, aggregate.AbsentDocument(), nil)
	if outcome.err != nil || !outcome.commitAttempted || store.writes != 1 {
		t.Fatalf("recorded root was not recreated: %+v", outcome)
	}
	assertMutationTestFile(t, path, "opaque original")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("recreated root lost recorded mode: %v", err)
	}
	outcome = restoreDocumentDestination(t.Context(), authority, destination, comparison, before, &mode)
	if outcome.err != nil || outcome.commitAttempted || store.writes != 1 {
		t.Fatalf("recreated-root retry was not a no-op: %+v", outcome)
	}
}

func TestDocumentRestorationPublicationPreservesUnreadableRecordedMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged owner to exercise permission-dependent verification")
	}
	for _, recreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "replace", true: "recreate"}[recreate], func(t *testing.T) {
			authority, destination, path, store := restorationPublicationFixture(t, "readable expected")
			t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
			beforeMode, expectedMode := recovery.PermissionMode(0), recovery.PermissionMode(0o644)
			staged := aggregate.ExistingDocument([]byte("readable expected"))
			var stagedMode *recovery.PermissionMode = &expectedMode
			if recreate {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				staged, stagedMode = aggregate.AbsentDocument(), nil
			}
			comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{
				Before: aggregate.ExistingDocument([]byte("original content")), BeforeMode: &beforeMode,
				Expected: staged, ExpectedMode: stagedMode,
				Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
			})
			if err != nil {
				t.Fatal(err)
			}
			outcome := restoreDocumentDestination(t.Context(), authority, destination, comparison, staged, stagedMode)
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0 {
				t.Fatalf("recorded0000 mode was not restored: stat=%v publication=%v", err, outcome.err)
			}
			if outcome.err != nil || !outcome.commitAttempted || store.writes != 1 {
				t.Fatalf("unreadable-mode restoration did not complete: attempted=%v error=%v writes=%d", outcome.commitAttempted, outcome.err, store.writes)
			}
			if _, err := os.ReadFile(path); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("fixture did not exercise an unreadable file: %v", err)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			assertMutationTestFile(t, path, "original content")
		})
	}
}

func TestDocumentRestorationPublicationReportsFailureAfterVisibility(t *testing.T) {
	authority, destination, path, store := restorationPublicationFixture(t, restorationExpected)
	injected := errors.New("injected failure after replacement")
	store.afterReplace = func() error { return injected }
	mode := recovery.PermissionMode(0o644)
	outcome := restoreDocumentDestination(t.Context(), authority, destination, restorationPublicationComparison(t, mode), aggregate.ExistingDocument([]byte(restorationExpected)), &mode)
	if !errors.Is(outcome.err, injected) || !outcome.commitAttempted || !mutationfs.MayHaveVisibleEffect(outcome.err) || store.writes != 1 {
		t.Fatalf("visible failure was not retained as attempted/indeterminate: %+v", outcome)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertRestorationPublicationProjection(t, actual, []byte(restorationExpected))
}

func TestDocumentRestorationPublicationPreservesWholeMetadataAndRequiresRemovalAuthority(t *testing.T) {
	authority, destination, path, store := restorationPublicationFixture(t, "opaque expected")
	beforeMode, mode := recovery.PermissionMode(0o600), recovery.PermissionMode(0o644)
	input := journaldocument.ComparisonInput{
		Before: aggregate.ExistingDocument([]byte("opaque original")), Expected: aggregate.ExistingDocument([]byte("opaque expected")),
		BeforeMode: &beforeMode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	comparison, err := journaldocument.NewWholeComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	outcome := restoreDocumentDestination(t.Context(), authority, destination, comparison, input.Expected, &mode)
	if outcome.err != nil || !outcome.commitAttempted || store.writes != 1 {
		t.Fatalf("whole restoration failed: %+v", outcome)
	}
	assertMutationTestFile(t, path, "opaque original")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("whole restoration did not restore exact original mode: %v", err)
	}
	input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
	input.Expected, input.ExpectedMode = aggregate.ExistingDocument([]byte("opaque original")), &beforeMode
	comparison, err = journaldocument.NewWholeComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	outcome = restoreDocumentDestination(t.Context(), authority, destination, comparison, input.Expected, &beforeMode)
	if outcome.err == nil || store.writes != 1 {
		t.Fatalf("removal without journaled authority reached storage: %+v", outcome)
	}
	assertMutationTestFile(t, path, "opaque original")
}

func restorationPublicationFixture(t *testing.T, current string) (*Operation, mutationnamespace.Destination, string, *documentRestorationTestStore) {
	t.Helper()
	root := newProjectRootForMutationTest(t)
	path := filepath.Join(root, ".pi", "settings.json")
	writeMutationTestFile(t, path, current)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".pi/settings.json")
	store := &documentRestorationTestStore{Store: authority.filesystem}
	authority.filesystem = store
	return authority, destination, path, store
}

func restorationPublicationComparison(t *testing.T, mode recovery.PermissionMode) journaldocument.Comparison {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	id, found := aggregate.HostConfigCodecForSyntax(configdocument.JSON.String())
	if !found {
		t.Fatal("native JSON codec unavailable")
	}
	codec, err := nativecodec.New(t.Context(), id, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".pi/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	before, err := configdocument.Parse(t.Context(), []byte(restorationBefore), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := configdocument.Parse(t.Context(), []byte(restorationExpected), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	array, err := output.NewKeyPath("packages")
	if err != nil {
		t.Fatal(err)
	}
	order, err := configdocument.NewJSONArrayEdit(t.Context(), before, expected, array, nil, []int{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := journaldocument.NewSelectedComparison(t.Context(), journaldocument.ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(restorationBefore)), Expected: aggregate.ExistingDocument([]byte(restorationExpected)),
		BeforeMode: &mode, ExpectedMode: &mode, Limits: limits,
	}, []compose.Participant{participant}, []configdocument.JSONArrayEdit{order})
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}

func assertRestorationPublicationProjection(t *testing.T, actual, current []byte) {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	actualDocument, err := configdocument.Parse(t.Context(), actual, configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	for property, source := range map[string][]byte{"model": []byte(restorationBefore), "outside": current} {
		reference, err := configdocument.Parse(t.Context(), source, configdocument.JSON, limits)
		if err != nil {
			t.Fatal(err)
		}
		key, err := output.NewKeyPath(property)
		if err != nil {
			t.Fatal(err)
		}
		got, gotPresent, err := actualDocument.Lookup(key)
		if err != nil {
			t.Fatal(err)
		}
		want, wantPresent, err := reference.Lookup(key)
		if err != nil || gotPresent != wantPresent || !got.Equal(want) {
			t.Fatalf("restored %s differs from its owning source: %v", property, err)
		}
	}
	actualPi, err := piconfig.Parse(actual)
	if err != nil {
		t.Fatal(err)
	}
	beforePi, err := piconfig.Parse([]byte(restorationBefore))
	if err != nil || !actualPi.PackageArrayEqual(beforePi) {
		t.Fatalf("dedicated Pi array was not restored: %v", err)
	}
}

type documentRestorationTestStore struct {
	mutationfs.Store
	reads        int
	writes       int
	beforeRead   func(int)
	afterReplace func() error
}

func (store *documentRestorationTestStore) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	store.reads++
	if store.beforeRead != nil {
		store.beforeRead(store.reads)
	}
	return store.Store.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}

func (store *documentRestorationTestStore) ReplaceRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode, identity mutationfs.EntryIdentity) error {
	store.writes++
	if err := store.Store.ReplaceRootedFile(ctx, capability, content, mode, identity); err != nil {
		return err
	}
	if store.afterReplace != nil {
		return store.afterReplace()
	}
	return nil
}

func (store *documentRestorationTestStore) CreateRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode) error {
	store.writes++
	return store.Store.CreateRootedFile(ctx, capability, content, mode)
}

func (store *documentRestorationTestStore) RemoveRootedEntry(ctx context.Context, capability rootedpath.CommitCapability, identity mutationfs.EntryIdentity) error {
	store.writes++
	return errors.Join(errors.New("unexpected unjournaled restoration removal"), capability.Close())
}

func (store *documentRestorationTestStore) RemoveRootedEntryWithResidue(ctx context.Context, capability rootedpath.CommitCapability, identity mutationfs.EntryIdentity, names mutationfs.LogicalRemovalNames, limits mutationfs.TreeTraversalLimits) (mutationfs.CommitOutcome, error) {
	store.writes++
	return store.Store.RemoveRootedEntryWithResidue(ctx, capability, identity, names, limits)
}
