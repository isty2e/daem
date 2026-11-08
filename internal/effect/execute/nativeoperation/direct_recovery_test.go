package nativeoperation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit/aggregatefixture"
)

func TestExecuteNativeRecoveryReopensRetainedOrderTransaction(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, committed := range []bool{false, true} {
			phase := "before-state-rollback"
			if committed {
				phase = "committed-state-cleanup"
			}
			t.Run(string(scope)+"/"+phase, func(t *testing.T) {
				input, host, original := nativeOrderApplyInput(t, scope)
				ordinaryHost := ""
				if scope == target.ScopeProject && !committed {
					input, ordinaryHost = nativeOrderWithOrdinaryMCP(t, input)
				}
				plan, err := PrepareNativeApplyEffectPlan(input)
				if err != nil {
					t.Fatal(err)
				}

				failure := errors.New("selected native interruption")
				refusal := errors.New("automatic compensation authority refused")
				reached := false
				if !t.Run("interrupted-apply", func(t *testing.T) {
					options := nativeOrderApplyOptions(t)
					accept := options.AcceptVisibilityChanges
					options.AcceptVisibilityChanges = func(ctx context.Context) error {
						if err := accept(ctx); err != nil {
							return err
						}

						current, err := os.ReadFile(host)
						if err != nil || bytes.Equal(current, original) {
							return err
						}
						if ordinaryHost != "" {
							if _, err := os.ReadFile(ordinaryHost); errors.Is(err, os.ErrNotExist) {
								return nil
							} else if err != nil {
								return err
							}
						}

						state, err := statefile.LoadOptionalWithOptions(ctx, input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
						if err != nil {
							return err
						}
						if state.Equal(input.Preparation.AfterState()) != committed {
							return nil
						}
						reached = true
						return failure
					}
					options.ValidateCompensationAuthority = func(context.Context) error { return refusal }
					result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, options)
					if !reached || !errors.Is(err, failure) || !errors.Is(err, refusal) || !result.ExecutionAttempted {
						t.Fatalf("interrupted native Apply did not preserve the selected refusal: %v", err)
					}
					if _, present := result.NativeSettlement(); present {
						t.Fatal("interrupted Apply returned completed settlement evidence")
					}
				}) {
					return
				}

				current, err := os.ReadFile(host)
				if err != nil || bytes.Equal(current, original) {
					t.Fatal("fixture did not retain a real host publication", err)
				}
				var ordinaryCurrent []byte
				if ordinaryHost != "" {
					ordinaryCurrent, err = os.ReadFile(ordinaryHost)
					if err != nil {
						t.Fatal("fixture did not retain its ordinary host publication", err)
					}
				}
				entries, err := os.ReadDir(input.Paths.RecoveryDir)
				if err != nil || len(entries) != 1 {
					t.Fatal("refused compensation did not retain one journal", err)
				}
				if err := input.ProjectRoot.Close(); err != nil {
					t.Fatal(err)
				}

				store := &nativeDirectRecoveryStore{Store: testFilesystem(), host: host}
				native, load := nativeOrderRecoveryPlan(t, input, host, store)

				leaseStore, err := mutation.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}

				requests := []mutation.PhysicalAuthorityRequest{{Path: host, Target: string(target.TargetPi), Scope: string(scope)}}
				if ordinaryHost != "" {
					requests = append(requests, mutation.PhysicalAuthorityRequest{Path: ordinaryHost, Target: string(target.TargetClaudeCode), Scope: string(target.ScopeProject)})
				}
				var domains []mutation.Domain
				for _, request := range requests {
					for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
						domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: request.Path, Access: mutation.AccessExclusive, Effect: effect, Target: request.Target, Scope: request.Scope})
						if err != nil {
							t.Fatal(err)
						}
						domains = append(domains, domain)
					}
				}
				leases, err := leaseStore.Acquire(t.Context(), domains...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := leases.Release(); err != nil {
						t.Error(err)
					}
				})

				validated, accepted := 0, 0
				options := execute.RecoveryOptions{
					Resolver: input.Resolver, Filesystem: store, StateCodec: input.StateCodec, Codecs: input.Codecs,
					OwnershipRegistryBinder: input.OwnershipRegistryBinder,
					ValidateBeforeEffects: func(_ context.Context, selected mutation.PhysicalAuthoritySet) error {
						validated++
						namespaces, err := selected.Namespaces()
						if err != nil || len(namespaces) == 0 {
							return errors.Join(errors.New("explicit order recovery requires physical authority"), err)
						}
						covered, err := leases.CoversPhysicalAuthority(selected)
						if err != nil || !covered {
							return errors.Join(errors.New("explicit recovery lacks selected physical authority"), err)
						}
						return nil
					},
					ValidateVisibilityAuthority: func(ctx context.Context) error {
						matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
						if err != nil || !matches {
							return errors.Join(errors.New("explicit recovery visibility changed"), err)
						}
						return nil
					},
					AcceptVisibilityChanges: func(ctx context.Context) error {
						accepted++
						matches, err := leases.AcceptVisibilityChanges(ctx)
						if err != nil || !matches {
							return errors.Join(errors.New("explicit recovery visibility acceptance failed"), err)
						}
						return nil
					},
				}
				if scope == target.ScopeProject && !committed {
					denied := errors.New("explicit recovery authority refused")
					refused := options
					refused.ValidateBeforeEffects = func(ctx context.Context, selected mutation.PhysicalAuthoritySet) error {
						if err := options.ValidateBeforeEffects(ctx, selected); err != nil {
							return err
						}
						return denied
					}
					err := ExecuteNativeRecovery(t.Context(), native, input.Paths, load, refused)
					if !errors.Is(err, denied) || store.hostWrites != 0 || accepted != 0 {
						t.Fatalf("refused explicit recovery changed visibility: %v; writes=%d acceptances=%d", err, store.hostWrites, accepted)
					}
					actual, err := os.ReadFile(host)
					if err != nil || !bytes.Equal(actual, current) {
						t.Fatal("refused explicit recovery changed its host", err)
					}
					if ordinaryHost != "" {
						actual, err := os.ReadFile(ordinaryHost)
						if err != nil || !bytes.Equal(actual, ordinaryCurrent) {
							t.Fatal("refused explicit recovery changed its ordinary participant", err)
						}
					}
					state, err := statefile.LoadOptionalWithOptions(t.Context(), input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
					if err != nil || !state.Equal(input.CurrentState) {
						t.Fatal("refused explicit recovery changed durable state", err)
					}
					entries, err := os.ReadDir(input.Paths.RecoveryDir)
					if err != nil || len(entries) != 1 {
						t.Fatal("refused explicit recovery removed its journal", err)
					}

					native, load = nativeOrderRecoveryPlan(t, input, host, store)
					validated, accepted = 0, 0
				}

				err = ExecuteNativeRecovery(t.Context(), native, input.Paths, load, options)
				if err != nil || validated == 0 || accepted == 0 {
					t.Fatalf("fresh explicit native recovery failed: %v; validations=%d acceptances=%d", err, validated, accepted)
				}

				wantedState, wantedHost, wantedWrites := input.CurrentState, original, 1
				if committed {
					wantedState, wantedHost, wantedWrites = input.Preparation.AfterState(), current, 0
				}
				actual, err := os.ReadFile(host)
				if err != nil || !bytes.Equal(actual, wantedHost) || store.hostWrites != wantedWrites {
					t.Fatalf("explicit recovery host result: error=%v writes=%d want=%d", err, store.hostWrites, wantedWrites)
				}
				info, err := os.Stat(host)
				if err != nil || info.Mode().Perm() != 0o644 {
					t.Fatal("explicit recovery changed host permissions", err)
				}
				if ordinaryHost != "" {
					if _, err := os.Stat(ordinaryHost); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("explicit recovery did not restore ordinary participant absence", err)
					}
				}

				state, err := statefile.LoadOptionalWithOptions(t.Context(), input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil)})
				if err != nil || !state.Equal(wantedState) {
					t.Fatal("explicit recovery changed the selected durable state", err)
				}
				registry, err := load.OwnershipRegistry.Load(t.Context())
				if err != nil || !registry.Equal(input.Registry) {
					t.Fatal("explicit order recovery changed ownership membership", err)
				}
				entries, err = os.ReadDir(input.Paths.RecoveryDir)
				if err != nil || len(entries) != 0 {
					t.Fatal("explicit recovery did not retire its evidence", err)
				}
			})
		}
	}
}

func nativeOrderRecoveryPlan(t *testing.T, input NativeApplyInput, host string, filesystem mutationfs.Store) (journal.NativeRecoveryPlan, journal.PlanLoadOptions) {
	t.Helper()
	budget := physicalwork.NewCaptureBudget(nil)
	reader, err := ownershipstore.NewRecoveryReader(input.Paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	readState := func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptionalWithOptions(ctx, input.Paths.StatefilePath, statefile.ReadOptions{Codec: input.StateCodec, PhysicalPathBudget: budget})
	}
	piRoot, err := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	load := journal.PlanLoadOptions{
		Resolver: func(destination output.Destination) (string, error) { return input.Resolver(destination, budget) }, Filesystem: filesystem, OwnershipRegistry: reader,
		StateReader: readState, StateCodec: input.StateCodec, Codecs: input.Codecs, PhysicalPathBudget: budget,
		NativeDocuments: &journal.Profile{
			Protection: namespacetest.Reservations(t),
			StateCodec: input.StateCodec, Codecs: input.Codecs, StateReader: readState, Budget: budget,
			Manifest: input.Paths.ManifestRoot, ManifestPath: input.Owner.ManifestPath(),
			Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
			ResolveRoot: func(reference output.RootReference) (string, error) {
				if reference == piRoot {
					return filepath.Dir(host), nil
				}
				return "", errors.New("order recovery has no selected root for this role")
			},
		},
	}
	disclosed, err := journal.LoadRecoverablePlanWithOptions(t.Context(), input.Paths.JournalPaths(), load)
	if err != nil {
		t.Fatal(err)
	}
	native, ok := disclosed.(journal.NativeRecoveryPlan)
	if !ok || native.Blocked() || native.HasErrors() {
		t.Fatalf("retained operation did not disclose executable native recovery: %#v", disclosed)
	}
	return native, load
}

func nativeOrderWithOrdinaryMCP(t *testing.T, input NativeApplyInput) (NativeApplyInput, string) {
	t.Helper()
	decisions := aggregatefixture.ClaudeProjectMCP(t, aggregate.AbsentDocument(), []aggregatefixture.MCPEntry{{ServerID: "mixed", Command: "mixed-server"}})
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: input.CurrentState, Aggregates: decisions, PiOrders: input.Preparation.PiOrders(), StateCodec: input.StateCodec})
	if err != nil {
		t.Fatal(err)
	}
	effects := prepared.AggregateEffects()
	if len(effects) != 1 {
		t.Fatal("mixed recovery fixture requires one ordinary aggregate")
	}
	input.Preparation = prepared
	ordinary := effects[0].DocumentAddress().AggregateRoot()
	host := filepath.Join(input.Paths.ManifestRoot, ".mcp.json")
	resolve := input.Resolver
	input.Resolver = func(destination output.Destination, budget rootedpath.PhysicalTraversalBudget) (string, error) {
		if destination == ordinary {
			return host, nil
		}
		return resolve(destination, budget)
	}
	return input, host
}

type nativeDirectRecoveryStore struct {
	mutationfs.Store
	host       string
	hostWrites int
}

func (store *nativeDirectRecoveryStore) ReplaceRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode, expected mutationfs.EntryIdentity) error {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return err
	}
	if path == store.host {
		store.hostWrites++
	}
	return store.Store.ReplaceRootedFile(ctx, capability, content, mode, expected)
}
