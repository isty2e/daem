package statefile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeDocumentCodecRoundTripsMixedSnapshotsWithoutOrdinaryAdmission(t *testing.T) {
	contribution := contributionCodecState(t, configdocument.JSON)
	catalog, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{contribution.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewNativeCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := NewCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, selectedTarget := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, mode := range []os.FileMode{0, 0o600, 0o644} {
				t.Run(fmt.Sprintf("%s/%s/%04o", selectedTarget, scope, mode), func(t *testing.T) {
					baseline := nativeDocumentCodecBaseline(t, selectedTarget, scope, mode)
					snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
						ManagedPaths:      append(testV9Snapshot(t).ManagedPaths(), baseline),
						ManagedAggregates: []durable.ManagedAggregateState{contribution},
					})
					if err != nil {
						t.Fatal(err)
					}
					content, err := codec.Encode(snapshot)
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := codec.Decode(content)
					if err != nil || !snapshot.Equal(decoded) {
						t.Fatalf("native snapshot round trip differs: %v", err)
					}
					reencoded, err := codec.Encode(decoded)
					if err != nil || !bytes.Equal(content, reencoded) {
						t.Fatalf("native encoding is not deterministic: %v", err)
					}
					for _, encode := range []func(durable.Snapshot) ([]byte, error){Marshal, Codec{}.Encode, ordinary.Encode} {
						if result, err := encode(snapshot); err == nil || len(result) != 0 {
							t.Fatal("native factory widened ordinary whole-file encoding")
						}
					}
					for _, decode := range []func([]byte) (durable.Snapshot, error){Decode, Codec{}.Decode, ordinary.Decode} {
						if result, err := decode(content); err == nil || !result.Equal(durable.EmptySnapshot()) {
							t.Fatal("ordinary reader admitted native ownership or returned partial state")
						}
					}
				})
			}
		}
	}
}

func TestNativeDocumentCodecRefusesUncorrelatedAndReservedBaselines(t *testing.T) {
	codec, err := NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	baseline := nativeDocumentCodecBaseline(t, target.TargetPi, target.ScopeGlobal, 0o600)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{baseline}})
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*managedPathDTO)
	}{
		{"placement", func(row *managedPathDTO) { row.Subject.Namespace = "pi-host-config-project-v1" }},
		{"target", func(row *managedPathDTO) { row.ConsumerTargets = []string{"codex"} }},
		{"multiple targets", func(row *managedPathDTO) { row.ConsumerTargets = []string{"codex", "pi"} }},
		{"unsupported target", func(row *managedPathDTO) { row.ConsumerTargets = []string{"claude-code"} }},
		{"scope", func(row *managedPathDTO) { row.Scope = "project" }},
		{"foreign root", func(row *managedPathDTO) { row.Destination = "@codex-home/configs/native.data" }},
		{"ordinary root", func(row *managedPathDTO) { row.Destination = "~/configs/native.data" }},
		{"shared document", func(row *managedPathDTO) { row.Destination = "@pi-agent/settings.json" }},
		{"reserved code", func(row *managedPathDTO) { row.Destination = "@pi-agent/extensions/native.ts" }},
		{"reserved auth", func(row *managedPathDTO) { row.Destination = "@pi-agent/auth.json" }},
		{"directory", func(row *managedPathDTO) {
			row.ContentKind, row.PermissionPolicy, row.FileMode = "directory", "none", nil
		}},
		{"executable class", func(row *managedPathDTO) { row.PermissionPolicy, row.FileMode = "executable-class", nil }},
		{"executable mode", func(row *managedPathDTO) { mode := uint32(0o700); row.FileMode = &mode }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			persisted := persistedSnapshot(snapshot)
			mutation.edit(&persisted.ManagedPaths[0])
			row := persisted.ManagedPaths[0]
			if state, err := row.canonical(); err == nil {
				invalid, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{state}})
				if err != nil {
					t.Fatal(err)
				}
				if content, err := codec.Encode(invalid); err == nil || len(content) != 0 {
					t.Fatal("native encoder accepted invalid family metadata")
				}
			}
			persisted.ManagedPaths = append(persistedSnapshot(testV9Snapshot(t)).ManagedPaths, row)
			content, err := json.Marshal(persisted)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := codec.Decode(content); err == nil || !result.Equal(durable.EmptySnapshot()) {
				t.Fatal("native reader admitted invalid metadata or returned earlier ordinary rows")
			}
		})
	}
}

func TestNativeDocumentCodecPreservesOrdinaryVersionsAndClosesLegacyNativeRows(t *testing.T) {
	codec, err := NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	ordinary := testV9Snapshot(t)
	plain, err := Marshal(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(ordinary)
	if err != nil || !bytes.Equal(plain, encoded) {
		t.Fatalf("native capability changed ordinary bytes: %v", err)
	}
	for _, version := range []int{legacySnapshotVersion, previousSnapshotVersion, snapshotVersion} {
		input := stateVersionFixture(t, ordinary, version)
		expected, err := Decode(input)
		if err != nil {
			t.Fatal(err)
		}
		if actual, err := codec.Decode(input); err != nil || !actual.Equal(expected) {
			t.Fatalf("native capability changed ordinary version%d normalization: %v", version, err)
		}
	}
	baseline := nativeDocumentCodecBaseline(t, target.TargetCodex, target.ScopeProject, 0)
	native, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{baseline}})
	if err != nil {
		t.Fatal(err)
	}
	persisted := persistedSnapshot(native)
	for _, version := range []int{legacySnapshotVersion, previousSnapshotVersion} {
		persisted.Version = version
		content, err := json.Marshal(persisted)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := codec.Decode(content); err == nil || !result.Equal(durable.EmptySnapshot()) {
			t.Fatalf("native codec widened frozen version%d", version)
		}
	}
	if result, err := NewNativeCodec(aggregate.ContributionCatalog{}); err == nil || result != nil {
		t.Fatal("invalid native factory returned a usable codec")
	}
}

func TestNativeDocumentCodecKeepsStrictFrameAndSelectedValueAdmission(t *testing.T) {
	jsonState := contributionCodecState(t, configdocument.JSON)
	tomlState := contributionCodecState(t, configdocument.TOML)
	catalog, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{jsonState.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewNativeCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	baseline := nativeDocumentCodecBaseline(t, target.TargetPi, target.ScopeGlobal, 0o600)
	mixed, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths:      []durable.ManagedPathState{baseline},
		ManagedAggregates: []durable.ManagedAggregateState{jsonState, tomlState},
	})
	if err != nil {
		t.Fatal(err)
	}
	if content, err := codec.Encode(mixed); err == nil || len(content) != 0 {
		t.Fatal("native document capability admitted an unselected value codec")
	}
	unchecked, err := json.Marshal(persistedSnapshot(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := codec.Decode(unchecked); err == nil || !result.Equal(durable.EmptySnapshot()) {
		t.Fatal("unselected later value yielded native or partial state")
	}
	valid, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: mixed.ManagedPaths(), ManagedAggregates: []durable.ManagedAggregateState{jsonState}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := codec.Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), content...), []byte("\n{}")...),
		append([]byte(`{"unexpected":true,`), content[1:]...),
		append([]byte(`{"version":10,`), content[1:]...),
		append(append([]byte(nil), content...), bytes.Repeat([]byte(" "), int(maximumStatefileBytes))...),
	} {
		if result, err := codec.Decode(malformed); err == nil || !result.Equal(durable.EmptySnapshot()) {
			t.Fatal("native factory weakened strict framing or returned partial state")
		}
	}
}

func nativeDocumentCodecBaseline(t *testing.T, selectedTarget target.Target, scope target.Scope, mode os.FileMode) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, selectedTarget, scope)
	if err != nil {
		t.Fatal(err)
	}
	root := "." + string(selectedTarget)
	if scope == target.ScopeGlobal {
		root = "@codex-home"
		if selectedTarget == target.TargetPi {
			root = "@pi-agent"
		}
	}
	destination := outputtest.Parse(t, root+"/configs/native.data")
	state, err := durable.NewManagedPathState(subject, []target.Target{selectedTarget}, scope, destination,
		artifact.HashFileContent([]byte("opaque native document")), realization.PathProjectionFile, realization.PathPermissionsExact, mode)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
