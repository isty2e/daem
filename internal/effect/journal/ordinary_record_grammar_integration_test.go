package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const grammarOperationID = "20260625T000000.000000000Z-apply"

func ordinaryGrammarJournalBytes(t *testing.T) []byte {
	t.Helper()
	id, err := entity.New(entity.KindInstructions, "grammar")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	hash := artifact.HashFileContent([]byte("before\n"))
	previous, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := journalrecord.NewManagedPathRemoveMutation(previous, hash)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(subject, destination, true, hash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := journalrecord.NormalizeCaptureMutations([]journalrecord.ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{previous}})
	if err != nil {
		t.Fatal(err)
	}
	after := durable.EmptySnapshot()
	root, err := rootedpath.NewAuthorityProvenance(t.TempDir(), testContentHashString("grammar object"), testContentHashString("grammar mount"))
	if err != nil {
		t.Fatal(err)
	}
	beforePath := recovery.BeforePathState{Existed: true, PathMode: recovery.NewPermissionMode(0o600), Kind: recovery.PathKindFile, ContentHash: string(hash), BackupPath: "files/000001"}
	state, err := recovery.NewBeforeRemovalState(beforePath)
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatal(err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-00000000000000000000000000000001", ".daem-cleanup-00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := recovery.NewRootProvenance(root.PhysicalRoot(), root.ObjectFingerprint(), root.MountFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := recovery.NewExistingParentAuthority(provenance, names)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := journalrecord.NewOrdinaryDraft(grammarOperationID, time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC), before, after, root, mutations, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(beforePath); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents([]recovery.RemovalIntent{intent}); err != nil {
		t.Fatal(err)
	}
	if err := draft.BeginClaimCoverage(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil || present {
		t.Fatal("unexpected project claim demand", err)
	}
	completed, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, afterBytes, err := journalrecord.EncodeSnapshots(before, after, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content, err := completed.EncodeOrdinary(beforeBytes, afterBytes)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestZeroRecordedPathFactsRefuseBeforeObservation(t *testing.T) {
	observations, err := recoveryPathObservations(t.Context(), []journalrecord.PathEntry{{}}, nil, nil, nil, nil, aggregate.CodecCatalog{}, nil)
	if err == nil || observations != nil {
		t.Fatal("zero path view reached observation or supplied a prefix")
	}
	if err := validateRecoveryGlobalPathBindings(t.Context(), []journalrecord.PathEntry{{}}, nil, nil, nil); err == nil {
		t.Fatal("zero entry passed complete global binding validation")
	}
}

func TestRecordedPathObservationKeepsEntryFailureInItsDiagnostic(t *testing.T) {
	content := ordinaryGrammarJournalBytes(t)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, journalrecord.FileName)
	if err := os.WriteFile(path, content, journalrecord.Mode); err != nil {
		t.Fatal(err)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), path, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, path, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	ordinary, present := loaded.Ordinary()
	if !present {
		t.Fatal("ordinary fixture decoded as another family")
	}
	basis, err := ordinary.RecoveryPlanningBasis(filepath.Dir(path), journalrecord.Fingerprint(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := recoveryPathObservations(t.Context(), basis.PlanningEntries(), nil, nil, nil, nil, aggregate.CodecCatalog{}, nil)
	if err != nil || len(observations) != 1 || observations[0].Path != "AGENTS.md" || observations[0].Error == "" {
		t.Fatal("missing project authority did not remain an entry-local observation error", err)
	}
}

func TestLoadRecoveryJournalRequiresEntryFieldPresence(t *testing.T) {
	content := ordinaryGrammarJournalBytes(t)
	fields := []struct {
		name   string
		parent string
		child  string
	}{
		{name: "subject", parent: "subject"},
		{name: "scope", parent: "scope"},
		{name: "path", parent: "path"},
		{name: "before", parent: "before"},
		{name: "expected_after", parent: "expected_after"},
		{name: "state_before", parent: "state_before"},
		{name: "state_expected_after", parent: "state_expected_after"},
		{name: "before.existed", parent: "before", child: "existed"},
		{name: "expected_after.existed", parent: "expected_after", child: "existed"},
		{name: "state_before.managed", parent: "state_before", child: "managed"},
		{
			name:   "state_expected_after.managed",
			parent: "state_expected_after",
			child:  "managed",
		},
	}
	for _, field := range fields {
		for _, state := range []struct {
			name   string
			asNull bool
			want   string
		}{
			{name: "missing", want: "is required"},
			{name: "null", asNull: true, want: "must not be null"},
		} {
			t.Run(field.name+"/"+state.name, func(t *testing.T) {
				mutated := mutateRecoveryEntryField(
					t,
					content,
					field.parent,
					field.child,
					state.asNull,
				)
				_, loadErr, planErr := malformedRecoveryJournalErrors(t, mutated)
				fieldName := field.parent
				if field.child != "" {
					fieldName = field.child
				}
				for label, err := range map[string]error{
					"load": loadErr,
					"plan": planErr,
				} {
					if err == nil ||
						!strings.Contains(err.Error(), `field "`+fieldName+`"`) ||
						!strings.Contains(err.Error(), state.want) {
						t.Fatalf(
							"%s error = %v, want field %q %s",
							label,
							err,
							fieldName,
							state.want,
						)
					}
				}
			})
		}
	}
}

func TestLoadRecoveryJournalRequiresCanonicalCleanupStageName(t *testing.T) {
	content := ordinaryGrammarJournalBytes(t)
	for _, test := range []struct {
		name   string
		mutate func(map[string]json.RawMessage)
		want   string
	}{
		{
			name: "missing",
			mutate: func(namespace map[string]json.RawMessage) {
				delete(namespace, "cleanup_name")
			},
			want: `field "cleanup_name" is required`,
		},
		{
			name: "null",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["cleanup_name"] = json.RawMessage("null")
			},
			want: `field "cleanup_name" must not be null`,
		},
		{
			name: "case alias",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["Cleanup_Name"] = namespace["cleanup_name"]
				delete(namespace, "cleanup_name")
			},
			want: "canonical ASCII lower_snake_case",
		},
		{
			name: "different token",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["cleanup_name"] = json.RawMessage(`".daem-cleanup-fedcba9876543210fedcba9876543210"`)
			},
			want: "same opaque token",
		},
		{
			name: "short token",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["cleanup_name"] = json.RawMessage(`".daem-cleanup-0123456789abcdef"`)
			},
			want: "128-bit lowercase hexadecimal token",
		},
		{
			name: "uppercase token",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["cleanup_name"] = json.RawMessage(`".daem-cleanup-0123456789ABCDEF0123456789ABCDEF"`)
			},
			want: "128-bit lowercase hexadecimal token",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var err error
			var document map[string]json.RawMessage
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			var intents []map[string]json.RawMessage
			if err := json.Unmarshal(document["removal_intents"], &intents); err != nil {
				t.Fatal(err)
			}
			if len(intents) == 0 {
				t.Fatal("default recovery journal has no removal intent")
			}
			var namespace map[string]json.RawMessage
			if err := json.Unmarshal(intents[0]["namespace_authority"], &namespace); err != nil {
				t.Fatal(err)
			}
			test.mutate(namespace)
			intents[0]["namespace_authority"], err = json.Marshal(namespace)
			if err != nil {
				t.Fatal(err)
			}
			document["removal_intents"], err = json.Marshal(intents)
			if err != nil {
				t.Fatal(err)
			}
			mutated, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, loadErr, planErr := malformedRecoveryJournalErrors(t, mutated)
			for label, err := range map[string]error{"load": loadErr, "plan": planErr} {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("%s error = %v, want %q", label, err, test.want)
				}
			}
		})
	}
}

func TestLoadRecoveryJournalRejectsNamespaceVariantFieldMixing(t *testing.T) {
	content := ordinaryGrammarJournalBytes(t)
	for _, test := range []struct {
		name   string
		mutate func(map[string]json.RawMessage)
	}{
		{
			name: "existing parent with retained ancestor facts",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["retained_ancestor_provenance"] = namespace["parent_provenance"]
				namespace["missing_suffix"] = json.RawMessage(`"project"`)
			},
		},
		{
			name: "initially absent parent with exact parent facts",
			mutate: func(namespace map[string]json.RawMessage) {
				namespace["variant"] = json.RawMessage(`"initially_absent_parent"`)
				namespace["retained_ancestor_provenance"] = namespace["parent_provenance"]
				namespace["missing_suffix"] = json.RawMessage(`"project"`)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]json.RawMessage
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			var intents []map[string]json.RawMessage
			if err := json.Unmarshal(document["removal_intents"], &intents); err != nil {
				t.Fatal(err)
			}
			var namespace map[string]json.RawMessage
			if err := json.Unmarshal(intents[0]["namespace_authority"], &namespace); err != nil {
				t.Fatal(err)
			}
			test.mutate(namespace)
			intents[0]["namespace_authority"] = mustMarshalRecoveryJSON(t, namespace)
			document["removal_intents"] = mustMarshalRecoveryJSON(t, intents)
			_, loadErr, planErr := malformedRecoveryJournalErrors(t, mustMarshalRecoveryJSON(t, document))
			for label, err := range map[string]error{"load": loadErr, "plan": planErr} {
				if err == nil || !strings.Contains(err.Error(), "invalid variant fields") {
					t.Fatalf("%s error = %v, want invalid variant fields", label, err)
				}
			}
		})
	}
}

func malformedRecoveryJournalErrors(
	t *testing.T,
	content []byte,
) (string, error, error) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recoveryRoot := filepath.Join(directory, "recovery")
	operationDir := filepath.Join(recoveryRoot, grammarOperationID)
	if err := os.MkdirAll(operationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(operationDir, journalrecord.FileName)
	if err := os.WriteFile(journalPath, content, journalrecord.Mode); err != nil {
		t.Fatal(err)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), journalPath, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	_, loadErr := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, journalPath, testStateCodec())
	plan, planErr := LoadRecoverablePlanWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot, ManifestRoot: filepath.Dir(recoveryRoot)},
		PlanLoadOptions{
			Filesystem: journalTestFilesystem(),
			StateCodec: testStateCodec(),
		},
	)
	if plan != nil {
		t.Fatalf("malformed recovery journal produced plan %#v", plan)
	}
	return journalPath, loadErr, planErr
}

func mutateRecoveryEntryField(
	t *testing.T,
	content []byte,
	parent string,
	child string,
	asNull bool,
) []byte {
	t.Helper()
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(document["entries"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	target := entries[0]
	field := parent
	if child != "" {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(target[parent], &nested); err != nil {
			t.Fatal(err)
		}
		target = nested
		field = child
	}
	if asNull {
		target[field] = json.RawMessage(`null`)
	} else {
		delete(target, field)
	}
	if child != "" {
		entries[0][parent] = mustMarshalRecoveryJSON(t, target)
	}
	document["entries"] = mustMarshalRecoveryJSON(t, entries)
	return mustMarshalRecoveryJSON(t, document)
}

func mustMarshalRecoveryJSON(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func journalWithVersion(t *testing.T, content []byte, version int) []byte {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["version"] = json.RawMessage(fmt.Sprint(version))
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestRecoveryJournalRefusesV13AndRetainsItsEvidence(t *testing.T) {
	legacy := journalWithVersion(t, ordinaryGrammarJournalBytes(t), 13)
	path, decodeErr, planErr := malformedRecoveryJournalErrors(t, legacy)
	for label, err := range map[string]error{"decode": decodeErr, "plan": planErr} {
		if err == nil || !strings.Contains(err.Error(), "unsupported recovery journal version 13") || !strings.Contains(err.Error(), "use the daem version that wrote it") {
			t.Fatalf("%s: old recovery journal lost its version refusal or recovery guidance: %v", label, err)
		}
	}
	retained, err := os.ReadFile(path)
	if err != nil || string(retained) != string(legacy) {
		t.Fatalf("version refusal changed recovery evidence: %v", err)
	}
}
