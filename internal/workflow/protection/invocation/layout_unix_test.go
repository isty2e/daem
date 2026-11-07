//go:build darwin || linux

package invocation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/ownershiptest"
)

func prospectiveLayoutFixture(t *testing.T, fixture ownershiptest.PermissionRecoveryFixture, input InvocationInput, createsSecond bool, budget *physicalwork.Budget) (InvocationLayout, *physicalwork.Budget) {
	t.Helper()
	var rows []durable.ManagedAggregateState
	var aliases []aggregate.DocumentAddress
	for index, document := range fixture.Protection.Documents() {
		binding := document.Bindings()[0]
		selectedTarget, codec := target.TargetCodex, aggregate.CodecContractID("native-toml-v1")
		if index == 1 {
			selectedTarget, codec = target.TargetPi, "native-json-v1"
		}
		key, err := output.NewKeyPath("fixture")
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(selectedTarget, target.ScopeProject, binding.Destination(), footprint, codec)
		if err != nil {
			t.Fatal(err)
		}
		id, err := entity.New(entity.KindHostConfig, fmt.Sprintf("fixture-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyprojection.HostConfigKeySubject(id, selectedTarget, target.ScopeProject, key)
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := aggregate.NewManagedContribution(contract, `"fixture"`)
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
		aliases = append(aliases, contract.Address().Document())
	}
	beforeRows := rows
	if createsSecond {
		beforeRows = rows[:1]
		if err := os.Remove(fixture.Documents[1]); err != nil {
			t.Fatal(err)
		}
		current, err := fixture.Store.LoadProtectionRegistry(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		binding := fixture.Protection.Documents()[1].Bindings()[0]
		value, _ := ownership.PresentDocumentBinding(binding)
		current, err = current.ApplyDocumentBinding(fixture.Owner.Key(), binding.Destination(), value, ownership.NoDocumentBinding())
		if err != nil {
			t.Fatal(err)
		}
		publishLayoutFixtureRegistry(t, fixture, current)
		if err := input.Leases.Release(); err != nil {
			t.Fatal(err)
		}
		input.Leases = invocationFixtureLeases(t, fixture.Paths, input.Requests)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: beforeRows})
	if err != nil {
		t.Fatal(err)
	}
	after, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(fixture.Paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if budget == nil {
		budget = physicalwork.NewCaptureBudget(nil)
	}
	batch, err := journalpublication.ObserveNativeDocuments(t.Context(), journalobservation.Input{
		Protection: namespacetest.Reservations(t),
		Owner:      fixture.Owner, Before: before, Aliases: aliases,
		Roots:      namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: {SelectedPath: fixture.Paths.ManifestRoot, Root: root}}),
		Filesystem: storagecommit.Adapter{}, Registry: fixture.Store,
		Limits: configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}, Budget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, members, err := batch.CaptureMembers(after, fixture.Protection.Correlation().OperationID())
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PrepareInvocationLayout(t.Context(), InvocationLayoutInput{
		Paths: input.Paths, Owner: fixture.Owner, Invocation: input.Protection.Invocation(), Correlation: input.Protection.Correlation(),
		Leases: input.Leases, Requests: input.Requests, Members: members,
	}, budget)
	if err != nil {
		t.Fatal(err)
	}
	return layout, budget
}

func publishLayoutFixtureRegistry(t *testing.T, fixture ownershiptest.PermissionRecoveryFixture, registry ownership.Registry) {
	t.Helper()
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.Paths.OwnershipRegistryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func publishLayoutFixtureCreation(t *testing.T, fixture ownershiptest.PermissionRecoveryFixture) {
	t.Helper()
	if err := os.WriteFile(fixture.Documents[1], []byte("malformed host payload\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.Documents[1], 0o640); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	binding := fixture.Protection.Documents()[1].Bindings()[0]
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(fixture.Documents[1])
	if err != nil {
		t.Fatal(err)
	}
	binding, err = ownership.NewDocumentBinding(fixture.Owner, binding.Destination(), observed.Exact())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := ownership.PresentDocumentBinding(binding)
	current, err = current.ApplyDocumentBinding(fixture.Owner.Key(), binding.Destination(), ownership.NoDocumentBinding(), value)
	if err != nil {
		t.Fatal(err)
	}
	publishLayoutFixtureRegistry(t, fixture, current)
}

func TestProspectiveInvocationAttachesActualNewMetadataBeforeCommand(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	layout, budget := prospectiveLayoutFixture(t, fixture, input, true, nil)
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	copy := layout
	if _, err := os.Stat(fixture.Documents[1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preparation created the future document", err)
	}
	publishLayoutFixtureCreation(t, fixture)
	attached, err := layout.AttachProtection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("attachment published an Arm", err)
	}
	documents := attached.Protection.Documents()
	if len(documents) != 2 || documents[0].ModeCap() != 0o600 || documents[1].ModeCap() != 0o640 || documents[1].UserID() != uint32(os.Getuid()) || documents[1].GroupID() != uint32(os.Getgid()) {
		t.Fatal("attachment invented or reused stale metadata")
	}
	if _, err := copy.AttachProtection(t.Context()); err == nil {
		t.Fatal("copied layout minted another attachment")
	}
	if _, err := budget.ReserveInvocation(physicalwork.InvocationWork{}, physicalwork.InvocationWork{}); err == nil {
		t.Fatal("attachment left admission open for unplanned work")
	}
	result, err := ExecuteInvocation(t.Context(), attached, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{
		Command: "/bin/sh", Args: []string{"-c", `/bin/chmod 664 "$1" "$2"`, "protected", fixture.Documents[0], fixture.Documents[1]},
	}, bind, nil)
	if !errors.Is(err, ErrInvocationPermissionsExpanded) {
		t.Fatal("actual post-prefix expansion was hidden", err)
	}
	process, present := result.Process()
	if !present || !process.ReleaseAttempted() || !process.Attempt().Succeeded() {
		t.Fatal("attached invocation did not execute the actual command")
	}
	record, present := result.Protection()
	if !present || record.Settlement() != protection.SettlementCleared {
		t.Fatal("attached invocation did not settle its safety obligation")
	}
	for index, path := range fixture.Documents {
		info, err := os.Stat(path)
		if err != nil || uint32(info.Mode().Perm()) != documents[index].ModeCap() {
			t.Fatal("containment did not use the actual post-prefix ceiling", err)
		}
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "malformed host payload\n" {
			t.Fatal("metadata settlement changed payload", err)
		}
	}
}

func TestProspectiveInvocationRefusesUncreatedTargetWithoutArming(t *testing.T) {
	fixture, input, _ := invocationFixture(t)
	layout, budget := prospectiveLayoutFixture(t, fixture, input, true, nil)
	if _, err := layout.AttachProtection(t.Context()); err == nil {
		t.Fatal("unreserved layout returned an invocation")
	}
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	attached, err := layout.AttachProtection(t.Context())
	if !errors.Is(err, os.ErrNotExist) || attached.qualification != nil {
		t.Fatal("uncreated document received metadata or invocation authority", err)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("failed attachment left a fence", err)
	}
	publishLayoutFixtureCreation(t, fixture)
	if _, err := layout.AttachProtection(t.Context()); err == nil {
		t.Fatal("failed attachment replenished its allocation")
	}
}

func TestProspectiveInvocationAttachesCreatedUnicodeTarget(t *testing.T) {
	fixture, input, bind := invocationFixture(t, ".codex/config.toml", ".pi/Futuré.json")
	layout, budget := prospectiveLayoutFixture(t, fixture, input, true, nil)
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	publishLayoutFixtureCreation(t, fixture)
	attached, err := layout.AttachProtection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if attached.Protection.Documents()[1].ModeCap() != 0o640 {
		t.Fatal("new Unicode target did not supply its actual metadata")
	}
	result, err := ExecuteInvocation(t.Context(), attached, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/usr/bin/true"}, bind, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record, present := result.Protection(); !present || record.Settlement() != protection.SettlementCleared {
		t.Fatal("new Unicode target did not settle after the actual command")
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("new Unicode target left a fence", err)
	}
}

func TestProspectiveInvocationRetainsForeignCohortResolutionContext(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	foreignPaths, err := daempaths.Resolve(filepath.Join(fixture.Paths.ManifestRoot, "foreign", "daem.toml"))
	if err == nil {
		foreignPaths, err = foreignPaths.WithDataDir(fixture.Paths.DataDir)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(foreignPaths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignPaths.StatefilePath, []byte("foreign state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(foreignPaths.ManifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.pi", filepath.Join(foreignPaths.ManifestRoot, ".pi")); err != nil {
		t.Fatal(err)
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthority(foreignPaths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), foreignPaths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	selected := fixture.Protection.Documents()[1].Bindings()[0]
	foreign, err := ownership.NewDocumentBinding(owner, selected.Destination(), selected.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current, err = ownership.NewRegistry(current.Claims(), append(current.DocumentBindings(), foreign)...)
	if err != nil {
		t.Fatal(err)
	}
	publishLayoutFixtureRegistry(t, fixture, current)
	layout, budget := prospectiveLayoutFixture(t, fixture, input, false, nil)
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	attached, err := layout.AttachProtection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(attached.Protection.Documents()[1].Bindings()) != 2 {
		t.Fatal("attachment discarded the differently rooted foreign participant")
	}
	result, err := ExecuteInvocation(t.Context(), attached, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/usr/bin/true"}, bind, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertInvocationSettled(t, fixture, result)
}

func TestProspectiveInvocationRefusesUnplannedCohortChange(t *testing.T) {
	fixture, input, _ := invocationFixture(t)
	layout, budget := prospectiveLayoutFixture(t, fixture, input, false, nil)
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	alias, err := output.Parse(".pi/unplanned.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(fixture.Owner, alias, fixture.Protection.Documents()[1].PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	current, err = ownership.NewRegistry(current.Claims(), append(current.DocumentBindings(), binding)...)
	if err != nil {
		t.Fatal(err)
	}
	publishLayoutFixtureRegistry(t, fixture, current)
	attached, err := layout.AttachProtection(t.Context())
	var refusal *ownership.ProtectionRefusal
	if !errors.As(err, &refusal) || attached.qualification != nil {
		t.Fatal("unplanned cohort received invocation authority", err)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("cohort refusal left a fence", err)
	}
}

func TestProspectiveInvocationsShareAdmissionButNotTransferredCredit(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	first, budget := prospectiveLayoutFixture(t, fixture, input, false, nil)
	invocation, err := executionidentity.NewInvocation(strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}
	input.Protection, err = ownership.NewArmedProtection(fixture.Owner, invocation, input.Protection.Correlation(), input.Protection.Documents())
	if err != nil {
		t.Fatal(err)
	}
	second, _ := prospectiveLayoutFixture(t, fixture, input, false, budget)
	for _, layout := range []InvocationLayout{first, second} {
		if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(fixture.Paths.ManifestRoot, "command-count")
	request := subprocess.CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `printf x >> "$1"`, "protected", marker}}
	for _, layout := range []InvocationLayout{first, second} {
		attached, err := layout.AttachProtection(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		result, err := ExecuteInvocation(t.Context(), attached, subprocess.CommandExecutor{}, request, bind, nil)
		if err != nil {
			t.Fatal(err)
		}
		assertInvocationSettled(t, fixture, result)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "xx" {
		t.Fatal("prepaid peer commands were omitted or replayed", err)
	}
}
