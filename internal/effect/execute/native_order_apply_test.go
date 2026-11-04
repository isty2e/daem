package execute

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/journal"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestNativeApplyRetainsPiOrderOnlyWorkWithoutManagedRows(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, host, original := nativeOrderApplyInput(t, scope)
			work, err := nativeApplyWorkFor(input)
			if err != nil {
				t.Fatal(err)
			}
			if !work.changed || len(work.documents.Evidence().Namespaces()) != 1 || len(work.mutating) != 1 || !work.mutating[0] {
				t.Fatal("changed Pi order did not select its only settings writer")
			}
			before, after := input.Preparation.BeforeState(), input.Preparation.AfterState()
			if len(before.ManagedPaths())+len(before.ManagedAggregates())+len(after.ManagedPaths())+len(after.ManagedAggregates()) != 0 {
				t.Fatal("order-only preparation invented managed state")
			}
			if _, err := PrepareNativeApplyEffectPlan(input); err != nil {
				t.Fatal("selected order-only work did not compile an execution plan", err)
			}
			if count, err := MaximumNativeForwardEffectValidationCount(input); err != nil || count == 0 {
				t.Fatal("order-only work lost its visibility-validation schedule", err)
			}
			actual, err := os.ReadFile(host)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatal("order-only planning wrote the host", err)
			}
		})
	}
}

func TestNativeApplyPublishesPiOrderOnlyTransaction(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, host, _ := nativeOrderApplyInput(t, scope)
			plan, err := PrepareNativeApplyEffectPlan(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, nativeOrderApplyOptions(t))
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(host)
			if err != nil {
				t.Fatal(err)
			}
			if err := input.Preparation.PiOrders()[0].VerifyPostContent(actual, true); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(host)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatal("order-only publication changed host permissions", err)
			}
			state, err := statefile.LoadOptionalWithOptions(t.Context(), input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
			if err != nil || !state.Equal(input.Preparation.AfterState()) || !result.State.Equal(state) || result.ActionCount != 1 {
				t.Fatal("order-only publication lost its actual committed state", err)
			}
			reader, err := ownershipstore.NewRecoveryReader(input.Paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := reader.Load(t.Context())
			if err != nil || !registry.Equal(input.Registry) {
				t.Fatal("order-only publication invented ownership membership", err)
			}
			entries, err := os.ReadDir(input.Paths.RecoveryDir)
			if err != nil || len(entries) != 0 {
				t.Fatal("successful order-only publication left recovery work", err)
			}
		})
	}
}

func TestNativeApplyRollsBackPiOrderAfterReturnedPublicationError(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, host, original := nativeOrderApplyInput(t, scope)
			plan, err := PrepareNativeApplyEffectPlan(input)
			if err != nil {
				t.Fatal(err)
			}
			options := nativeOrderApplyOptions(t)
			accept := options.AcceptVisibilityChanges
			sentinel := errors.New("returned error after Pi order publication")
			rejected := false
			options.AcceptVisibilityChanges = func(ctx context.Context) error {
				if err := accept(ctx); err != nil {
					return err
				}
				current, err := os.ReadFile(host)
				if err != nil {
					return err
				}
				if !bytes.Equal(current, original) && !rejected {
					rejected = true
					return sentinel
				}
				return nil
			}
			result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, options)
			if !rejected || !errors.Is(err, sentinel) || !result.ExecutionAttempted || result.ActionCount != 0 {
				t.Fatalf("post-publication failure did not complete guarded rollback: %v", err)
			}
			actual, err := os.ReadFile(host)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatal("Pi rollback did not restore its actual original", err)
			}
			info, err := os.Stat(host)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatal("Pi rollback changed host permissions", err)
			}
			state, err := statefile.LoadOptionalWithOptions(t.Context(), input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
			if err != nil || !state.Equal(input.CurrentState) || !result.State.Equal(state) {
				t.Fatal("Pi rollback changed the committed Before state", err)
			}
			reader, err := ownershipstore.NewRecoveryReader(input.Paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := reader.Load(t.Context())
			if err != nil || !registry.Equal(input.Registry) {
				t.Fatal("Pi rollback invented ownership membership", err)
			}
			entries, err := os.ReadDir(input.Paths.RecoveryDir)
			if err != nil || len(entries) != 0 {
				t.Fatal("successful Pi rollback left recovery work", err)
			}
		})
	}
}

func nativeOrderApplyOptions(t *testing.T) ApplyOptions {
	t.Helper()
	store, err := mutation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var leases *mutation.LeaseSet
	t.Cleanup(func() {
		if leases != nil {
			if err := leases.Release(); err != nil {
				t.Error(err)
			}
		}
	})
	accept := func(ctx context.Context) error {
		accepted, err := leases.AcceptVisibilityChanges(ctx)
		if err != nil || !accepted {
			return errors.Join(errors.New("order fixture visibility changed"), err)
		}
		return nil
	}
	return ApplyOptions{
		ValidatePhysicalAuthorityRequests: func(ctx context.Context, requests []mutation.PhysicalAuthorityRequest) error {
			if leases == nil {
				var domains []mutation.Domain
				for _, request := range requests {
					for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
						domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: request.Path, Access: mutation.AccessExclusive, Effect: effect, Target: request.Target, Scope: request.Scope})
						if err != nil {
							return err
						}
						domains = append(domains, domain)
					}
				}
				var err error
				leases, err = store.Acquire(ctx, domains...)
				if err != nil {
					return err
				}
			}
			covered, err := leases.MatchCurrentPhysicalRequests(ctx, requests...)
			if err != nil || !covered {
				return errors.Join(errors.New("order fixture has no matching physical leases"), err)
			}
			return nil
		},
		AcceptVisibilityChanges: accept,
		ValidateCompensationAuthority: func(ctx context.Context) error {
			matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
			if err != nil || !matches {
				return errors.Join(errors.New("order fixture compensation authority changed"), err)
			}
			return nil
		},
		AcceptCompensationVisibilityChanges: accept,
	}
}

func nativeOrderApplyInput(t *testing.T, scope target.Scope) (NativeApplyInput, string, []byte) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(root, ".pi")
	if scope == target.ScopeGlobal {
		settings = filepath.Join(root, "agent")
	}
	if err := os.MkdirAll(settings, 0o700); err != nil {
		t.Fatal(err)
	}
	commandContext, err := extensiontopology.NewLocalSourceContext(root, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	settingsContext, err := extensiontopology.NewLocalSourceContext(settings, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	var relations []pipackage.ScopedRelation
	var members []hostrelation.RelationOrderMember
	for _, name := range []string{"a", "b"} {
		source := "npm:" + name + "@1"
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "pi.package-carrier", name)
		if err != nil {
			t.Fatal(err)
		}
		key, err := hostrelation.NewSubjectKey(source)
		if err != nil {
			t.Fatal(err)
		}
		managed, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + name)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(key, managed)
		if err != nil {
			t.Fatal(err)
		}
		correlation, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := pipackage.NewScopedRelationWithContext(correlation, scope, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
		identity, err := pipackage.HostLoadIdentityForContext(source, scope, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		load, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(subject, load)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
	}
	capability, present := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetPi, scope, desiredextension.CarrierPiPackage)
	if !present {
		t.Fatal("Pi order capability is missing")
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"packages":["npm:b@1","npm:foreign@1","npm:a@1"],"theme":"day"}`)
	order, err := pipackage.ReconstructOrder(pipackage.LoadedOrderInput{Scope: scope, SettingsContext: settingsContext, Constraint: constraint, Relations: relations, Document: aggregate.ExistingDocument(original)})
	if err != nil || !order.Changed() {
		t.Fatal("fixture has no changed Pi order", err)
	}
	host := filepath.Join(settings, "settings.json")
	if err := os.WriteFile(host, original, 0o644); err != nil {
		t.Fatal(err)
	}
	codecs := aggregatecodec.Catalog()
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	before := durable.EmptySnapshot()
	prepared, err := PrepareNativeApplyState(t.Context(), NativeApplyStateInput{Before: before, PiOrders: []pipackage.OrderObservation{order}, StateCodec: codec})
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "state.json")
	state, err := codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, state, 0o600); err != nil {
		t.Fatal(err)
	}
	stateObservation, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateObservation.Exact(), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "ownership.json")
	if err := os.WriteFile(registryPath, registryBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := ownershipstore.NewRecoveryReader(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manifest.Close() })
	roots := journalnamespace.Roots{Manifest: journalnamespace.RootSelection{Root: manifest, SelectedPath: root}}
	if scope == target.ScopeGlobal {
		agent, err := rootedpath.CaptureRoot(settings)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = agent.Close() })
		roots.PiAgent = journalnamespace.RootSelection{Root: agent, SelectedPath: settings}
	}
	alias, err := pipackage.SettingsDocumentAddress(scope)
	if err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	documents, err := journal.ObserveNativeDocuments(t.Context(), journalobservation.Input{Owner: owner, Before: before, Aliases: []aggregate.DocumentAddress{alias}, Roots: roots, Registry: reader, Filesystem: testFilesystem(), Limits: limits, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	return NativeApplyInput{
		ApplyInput: ApplyInput{
			CurrentState: before, Owner: owner, Codecs: codecs, StateCodec: codec, Filesystem: testFilesystem(), ProjectRoot: manifest,
			Paths: Paths{ManifestRoot: root, StateDir: root, StatefilePath: statePath, RecoveryDir: filepath.Join(root, "recovery"), DataDir: filepath.Join(root, "data"), OwnershipRegistryPath: registryPath},
			Resolver: func(destination output.Destination) (string, error) {
				if destination != alias.AggregateRoot() {
					return "", errors.New("order fixture has no selected destination")
				}
				return host, nil
			}, OwnershipRegistryBinder: testOwnershipRegistryBinder(),
		},
		Preparation: prepared, Documents: documents, Registry: registry, Budget: budget,
		RecoveryCodecs: func(context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, error) {
			return codecs, codec, nil
		},
	}, host, original
}
