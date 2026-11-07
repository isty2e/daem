package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeKeySourceProducesAtomicContractsAndExactSource(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, syntax := range []hostconfig.Syntax{hostconfig.SyntaxJSON, hostconfig.SyntaxJSONC, hostconfig.SyntaxJSONLineComments, hostconfig.SyntaxTOML} {
				t.Run(string(selected)+"/"+string(scope)+"/"+string(syntax), func(t *testing.T) {
					payload := `{"large":9007199254740993,"a/b":{"":true},"empty":{},"array":[1,null]}`
					want := map[string]bool{"/large": true, "/a~1b/": true, "/empty": true, "/array": true}
					if syntax == hostconfig.SyntaxTOML {
						payload = "large=9007199254740993\nwhen=2026-10-23\nempty={}\n[[array]]\nname='one'\n"
						want = map[string]bool{"/large": true, "/when": true, "/empty": true, "/array": true}
					} else if syntax != hostconfig.SyntaxJSON {
						payload = "// retained source comment\n" + payload
					}
					dir := t.TempDir()
					source := nativeLocalSource(t, dir, "source", []byte(payload), 0o755)
					config := nativeDocumentConfig(t, "native", source, selected, scope, "custom.conf", hostconfig.OwnershipKeys, syntax)
					environment := lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
					file, err := BuildNativeDocumentsWithOptions(t.Context(), environment, nativeDocumentResolver(t, dir), Options{}, nativeDocumentLimits())
					if err != nil {
						t.Fatal(err)
					}
					resource, present := file.Locked.ExactSupplySubject(config.ID())
					placement, placed := resource.HostConfigPlacement()
					if !present || !placed || placement.Ownership() != hostconfig.OwnershipKeys || placement.RequestedSyntax() != syntax || file.Locked.Len() != len(want)+1 {
						t.Fatal("native source mode/grammar or complete atomic set was lost")
					}
					view, err := access.OpenView(filepath.Join(dir, "source"))
					if err != nil {
						t.Fatal(err)
					}
					actual, err := view.Hash(t.Context())
					identity, _ := resource.ExactSupply()
					if err != nil || identity.ContentHash() != actual {
						t.Fatal("atomic refinement changed the retained exact source", err)
					}
					for _, contract := range file.Locked.Subjects() {
						spec, realized := contract.Realization()
						if !realized {
							continue
						}
						contribution, keyed := spec.ManagedAggregateContribution()
						if !keyed || !want[contribution.Address().KeyPath().String()] {
							t.Fatal("source produced a whole projection or non-atomic key")
						}
						delete(want, contribution.Address().KeyPath().String())
						item, err := aggregate.NewSubjectContribution(contract.SubjectID(), contribution)
						if err != nil {
							t.Fatal(err)
						}
						set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
						if err != nil || nativecodec.ValidateContributions(t.Context(), contribution.CodecContractID(), set, nativeDocumentLimits()) != nil {
							t.Fatal("source output is not a valid native atomic value", err)
						}
						for _, kind := range []lock.OperationKind{lock.OperationObserve, lock.OperationWriteProjection, lock.OperationRemoveProjection} {
							operation, found := contract.OperationContract(kind)
							if !found || (kind != lock.OperationObserve && operation.Route().AdapterContractVersion != string(contribution.CodecContractID())) {
								t.Fatal("native key operation lost its actual codec contract")
							}
						}
					}
					if len(want) != 0 {
						t.Fatal("source omitted a desired atomic key")
					}
					assertNativeKeyRoundtrip(t, dir, file)
				})
			}
		}
	}
}

func TestNativeKeySourceEmptyRootRetainsModeWithoutWholeOwnership(t *testing.T) {
	for _, input := range []struct {
		syntax hostconfig.Syntax
		text   string
	}{{hostconfig.SyntaxJSON, `{}`}, {hostconfig.SyntaxTOML, "# empty contribution\n"}} {
		dir := t.TempDir()
		source := nativeLocalSource(t, dir, "source", []byte(input.text), 0o600)
		config := nativeDocumentConfig(t, "empty", source, target.TargetPi, target.ScopeProject, "custom.conf", hostconfig.OwnershipKeys, input.syntax)
		file, err := BuildNativeDocumentsWithOptions(t.Context(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, dir), Options{}, nativeDocumentLimits())
		if err != nil || file.Locked.Len() != 1 {
			t.Fatal("empty key source disappeared or acquired a root projection", err)
		}
		resource, ok := file.Locked.ExactSupplySubject(config.ID())
		placement, placed := resource.HostConfigPlacement()
		if !ok || !placed || placement.Ownership() != hostconfig.OwnershipKeys {
			t.Fatal("empty source lost explicit key-mode intent")
		}
		assertNativeKeyRoundtrip(t, dir, file)
	}
}

func TestNativeKeySourceSharedDocumentUsesActualFootprints(t *testing.T) {
	for _, test := range []struct {
		name, first, second string
		whole, refuse       bool
	}{
		{name: "disjoint", first: `{"a":1}`, second: `{"b":2}`},
		{name: "literal slash differs from ancestry", first: `{"a/b":1}`, second: `{"a":{"b":2}}`},
		{name: "duplicate", first: `{"a":1}`, second: `{"a":1}`, refuse: true},
		{name: "ancestor", first: `{"a":{}}`, second: `{"a":{"b":2}}`, refuse: true},
		{name: "whole and selected", first: `{"a":1}`, second: `{"b":2}`, whole: true, refuse: true},
		{name: "whole and empty", first: `{"a":1}`, second: `{}`, whole: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			firstMode := hostconfig.OwnershipKeys
			if test.whole {
				firstMode = hostconfig.OwnershipDocument
			}
			first := nativeDocumentConfig(t, "a", nativeLocalSource(t, dir, "first", []byte(test.first), 0o600), target.TargetPi, target.ScopeProject, "custom.json", firstMode, hostconfig.SyntaxJSON)
			second := nativeDocumentConfig(t, "b", nativeLocalSource(t, dir, "second", []byte(test.second), 0o600), target.TargetPi, target.ScopeProject, "custom.json", hostconfig.OwnershipKeys, hostconfig.SyntaxJSON)
			sessions := 0
			resolver := &nativeSessionCounter{underlying: nativeDocumentResolver(t, dir), sessions: &sessions}
			file, err := BuildNativeDocumentsWithOptions(t.Context(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{first, second}}), resolver, Options{}, nativeDocumentLimits())
			if test.refuse {
				if err == nil || file.Version != 0 {
					t.Fatal("conflicting actual key ownership returned a lock")
				}
				return
			}
			if err != nil || sessions != 1 || lock.Validate(file) != nil {
				t.Fatal("disjoint/empty keys were rejected or acquired in separate epochs", err)
			}
		})
	}
}

func TestNativeKeyCollectionRejectsOrphanRetargetAndOperationForgery(t *testing.T) {
	dir := t.TempDir()
	config := nativeDocumentConfig(t, "native", nativeLocalSource(t, dir, "source", []byte(`{"a":1}`), 0o600), target.TargetPi, target.ScopeProject, "custom.json", hostconfig.OwnershipKeys, hostconfig.SyntaxJSON)
	file, err := BuildNativeDocumentsWithOptions(t.Context(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, dir), Options{}, nativeDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := file.Locked.ExactSupplySubject(config.ID())
	var key lock.LockedSubjectContract
	for _, contract := range file.Locked.Subjects() {
		if _, realized := contract.Realization(); realized {
			key = contract
		}
	}
	spec, _ := key.Realization()
	contribution, _ := spec.ManagedAggregateContribution()
	retarget, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: config.Target(), Scope: config.Scope(), Path: "other.json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	otherContract, err := profile.HostConfigProjectionContract(retarget, contribution.Footprint())
	if err != nil {
		t.Fatal(err)
	}
	otherValue, err := aggregate.NewManagedContribution(otherContract, contribution.CanonicalContribution())
	if err != nil {
		t.Fatal(err)
	}
	other, err := lock.NewHostConfigKeySubjectContract(config.ID(), key.SubjectID(), otherValue, retarget)
	if err != nil {
		t.Fatal(err)
	}
	observe, _ := key.OperationContract(lock.OperationObserve)
	forged, err := lock.NewLockedSubjectContract(lock.LockedSubjectContractInput{EntityID: config.ID(), SubjectID: key.SubjectID(), Realization: &spec, Ownership: key.Ownership(), OnAbsent: key.OnAbsent(), Replay: key.ReplayCoverage(), OperationContracts: []lock.OperationContract{observe}})
	if err != nil {
		t.Fatal(err)
	}
	for _, subjects := range [][]lock.LockedSubjectContract{{key}, {resource, other}, {resource, forged}} {
		if _, err := lock.NewLockedSection(subjects, nil); err == nil {
			t.Fatal("native transport bypassed source correlation or canonical operation admission")
		}
	}
}

func assertNativeKeyRoundtrip(t *testing.T, dir string, file lock.File) {
	t.Helper()
	codec := nativeKeyLockCodec(t, file, nativeDocumentLimits())
	encoded, err := codec.Marshal(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "native.lock.toml")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Load(t.Context(), path)
	if err != nil || lock.BuildDelta(file, decoded).HasChanges() {
		t.Fatal("native key lock lost durable facts", err)
	}
	again, err := codec.Marshal(t.Context(), decoded)
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatal("native key encoding was not canonical", err)
	}
	common, err := lockfile.Marshal(file)
	if err != nil || !bytes.Equal(encoded, common) {
		t.Fatal("common encoder disagrees with bounded codec", err)
	}
	commonFile, err := lockfile.Load(t.Context(), path)
	if err != nil || lock.BuildDelta(file, commonFile).HasChanges() {
		t.Fatal("common reader lost native key contracts", err)
	}
}

func nativeKeyLockCodec(t *testing.T, file lock.File, limits configdocument.Limits) lockfile.Codec {
	t.Helper()
	var required []aggregate.CodecContractID
	for _, contract := range file.Locked.Subjects() {
		if spec, present := contract.Realization(); present {
			if contribution, selected := spec.ManagedAggregateContribution(); selected {
				required = append(required, contribution.CodecContractID())
			}
		}
	}
	catalog := aggregatecodec.Catalog().Contributions()
	if len(required) != 0 {
		var err error
		catalog, err = aggregatecodec.ForContributions(t.Context(), required, limits)
		if err != nil {
			t.Fatal(err)
		}
	}
	codec, err := lockfile.NewCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func TestNativeKeyCodecKeepsValueAdmissionExplicitAndBounded(t *testing.T) {
	dir := t.TempDir()
	config := nativeDocumentConfig(t, "native", nativeLocalSource(t, dir, "source", []byte(`{"a":"literal-value"}`), 0o600), target.TargetPi, target.ScopeProject, "custom.json", hostconfig.OwnershipKeys, hostconfig.SyntaxJSON)
	file, err := BuildNativeDocumentsWithOptions(t.Context(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, dir), Options{}, nativeDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertNativeKeyRoundtrip(t, dir, file)
	limited := nativeKeyLockCodec(t, file, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 64})
	if content, err := limited.Marshal(t.Context(), file); err == nil || content != nil {
		t.Fatal("caller-owned native value limit was bypassed")
	}
	if result, err := limited.Load(t.Context(), filepath.Join(dir, "native.lock.toml")); err == nil || result.Version != 0 {
		t.Fatal("reader returned a lock after native value-budget failure")
	}
	codec := nativeKeyLockCodec(t, file, nativeDocumentLimits())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if content, err := codec.Marshal(ctx, file); !errors.Is(err, context.Canceled) || content != nil {
		t.Fatal("canceled native encoding exposed bytes", err)
	}
	if result, err := codec.Load(ctx, filepath.Join(dir, "native.lock.toml")); !errors.Is(err, context.Canceled) || result.Version != 0 {
		t.Fatal("canceled native loading exposed a lock", err)
	}
	resource, _ := file.Locked.ExactSupplySubject(config.ID())
	placement, _ := resource.HostConfigPlacement()
	var bad lock.LockedSubjectContract
	for _, contract := range file.Locked.Subjects() {
		if spec, present := contract.Realization(); present {
			contribution, _ := spec.ManagedAggregateContribution()
			invalid, err := aggregate.NewManagedContribution(contribution.Contract(), "not-a-native-value")
			if err != nil {
				t.Fatal(err)
			}
			bad, err = lock.NewHostConfigKeySubjectContract(config.ID(), contract.SubjectID(), invalid, placement)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	section, err := lock.NewLockedSection([]lock.LockedSubjectContract{resource, bad}, nil)
	if err != nil {
		t.Fatal("malformed-value fixture must reach concrete value admission", err)
	}
	malformed := file
	malformed.Locked = section
	if content, err := lockfile.Marshal(malformed); err == nil || content != nil {
		t.Fatal("common writer bypassed concrete native value validation")
	}
	if content, err := codec.Marshal(t.Context(), malformed); err == nil || content != nil {
		t.Fatal("static transport admission replaced native value validation")
	}
}
