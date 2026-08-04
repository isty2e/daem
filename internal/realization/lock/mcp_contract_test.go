package lock

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestMCPProjectionSubjectContractCoversEveryPlacement(t *testing.T) {
	placements := aggregate.ImplementedMCPPlacements()
	if len(placements) != 9 {
		t.Fatalf("implemented placements = %d, want 9", len(placements))
	}
	seenSubjects := make(map[topology.SubjectID]struct{}, len(placements))
	for _, placement := range placements {
		t.Run(string(placement.ID()), func(t *testing.T) {
			input := testMCPProjectionInput(t, placement, nil)
			contract, err := NewMCPProjectionSubjectContract(input)
			if err != nil {
				t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
			}
			serverID, ok := topologymcp.ServerID(contract.SubjectID())
			if contract.EntityID() != input.EntityID || !ok || serverID != input.ServerID {
				t.Fatalf("contract identity = %q/%q", contract.EntityID(), contract.SubjectID())
			}
			realization, ok := contract.Realization()
			if !ok {
				t.Fatal("MCP contract is missing realization")
			}
			contribution, ok := realization.ManagedAggregateContribution()
			if !ok {
				t.Fatal("MCP contract is not a managed aggregate contribution")
			}
			contentPath, err := placement.ContentPath(input.ServerID)
			if err != nil {
				t.Fatal(err)
			}
			if contribution.PlacementID() != string(placement.ID()) ||
				contribution.Target() != placement.Target() ||
				contribution.Scope() != placement.Scope() ||
				contribution.AggregateRoot() != placement.ConfigPath() ||
				contribution.ContentPath() != string(contentPath) ||
				contribution.CodecContractID() != placement.CodecContractID() ||
				contribution.CanonicalContribution() != input.CanonicalProjection {
				t.Fatalf("aggregate contribution = %#v", contribution)
			}
			if _, duplicate := seenSubjects[contract.SubjectID()]; duplicate {
				t.Fatalf("duplicate placement subject %q", contract.SubjectID())
			}
			seenSubjects[contract.SubjectID()] = struct{}{}
			if contract.OnAbsent() != OnAbsentRemoveBinding {
				t.Fatalf("on absent = %q", contract.OnAbsent())
			}
			for _, operation := range []OperationKind{OperationObserve, OperationWriteProjection, OperationRemoveProjection} {
				if _, ok := contract.OperationContract(operation); !ok {
					t.Fatalf("missing operation %q", operation)
				}
			}
		})
	}
}

func TestMCPProjectionSubjectContractRejectsIdentityAndGraphDrift(t *testing.T) {
	placement := mustTestMCPPlacement(t, aggregate.MCPPlacementCodexProject)
	base := testMCPProjectionInput(t, placement, nil)

	tests := []struct {
		name   string
		mutate func(*MCPProjectionSubjectInput)
		want   string
	}{
		{name: "wrong entity kind", mutate: func(input *MCPProjectionSubjectInput) {
			input.EntityID = mustContractEntityID(t, entity.KindSkill, input.ServerID)
		}, want: "does not match server"},
		{name: "wrong entity name", mutate: func(input *MCPProjectionSubjectInput) {
			input.EntityID = mustContractEntityID(t, entity.KindMCPServer, "other")
		}, want: "does not match server"},
		{name: "missing projection", mutate: func(input *MCPProjectionSubjectInput) {
			input.Graph = mustTopologyGraph(t, nil, nil)
		}, want: "is missing"},
		{name: "same server different placement", mutate: func(input *MCPProjectionSubjectInput) {
			other := mustTestMCPPlacement(t, aggregate.MCPPlacementCodexGlobal)
			input.Graph = testMCPProjectionGraph(t, other, input.LauncherCommand, nil)
		}, want: "is missing"},
		{name: "missing launcher", mutate: func(input *MCPProjectionSubjectInput) {
			subject, _ := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), input.EntityID.Name())
			input.Graph = mustTopologyGraph(t, []topology.SubjectID{subject}, nil)
		}, want: "requires exactly one launcher"},
		{name: "wrong launcher", mutate: func(input *MCPProjectionSubjectInput) {
			input.Graph = testMCPProjectionGraph(t, placement, "node", nil)
		}, want: "launcher"},
		{name: "unsupported absence", mutate: func(input *MCPProjectionSubjectInput) {
			input.RequestedOnAbsent = desiredmcp.OnAbsentKeep
		}, want: "must use remove-binding"},
		{name: "unknown placement", mutate: func(input *MCPProjectionSubjectInput) {
			input.PlacementID = "future-placement"
		}, want: "unsupported MCP placement"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			_, err := NewMCPProjectionSubjectContract(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMCPProjectionSubjectContractEnforcesCredentialPolicy(t *testing.T) {
	const credential = "CONTEXT7_API_TOKEN"
	claude := mustTestMCPPlacement(t, aggregate.MCPPlacementClaudeProject)
	claudeInput := testMCPProjectionInput(t, claude, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(claudeInput); err != nil {
		t.Fatalf("Claude credential reference rejected: %v", err)
	}

	claudeGlobal := mustTestMCPPlacement(t, aggregate.MCPPlacementClaudeGlobal)
	claudeGlobalInput := testMCPProjectionInput(t, claudeGlobal, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(claudeGlobalInput); err != nil {
		t.Fatalf("Claude global credential reference rejected: %v", err)
	}

	codexGlobal := mustTestMCPPlacement(t, aggregate.MCPPlacementCodexGlobal)
	codexGlobalInput := testMCPProjectionInput(t, codexGlobal, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(codexGlobalInput); err != nil {
		t.Fatalf("Codex global credential reference rejected: %v", err)
	}

	openCodeGlobal := mustTestMCPPlacement(t, aggregate.MCPPlacementOpenCodeGlobal)
	openCodeGlobalInput := testMCPProjectionInput(t, openCodeGlobal, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(openCodeGlobalInput); err != nil {
		t.Fatalf("OpenCode global credential reference rejected: %v", err)
	}

	antigravityGlobal := mustTestMCPPlacement(t, aggregate.MCPPlacementAntigravityGlobal)
	antigravityGlobalInput := testMCPProjectionInput(t, antigravityGlobal, []string{credential})
	antigravityContract, err := NewMCPProjectionSubjectContract(antigravityGlobalInput)
	if err != nil {
		t.Fatalf("Antigravity global credential reference rejected: %v", err)
	}
	if !slices.Equal(antigravityContract.MCPEnvironmentSources(), []string{credential}) {
		t.Fatalf(
			"Antigravity locked environment sources = %#v, want %q",
			antigravityContract.MCPEnvironmentSources(),
			credential,
		)
	}
	sources := antigravityContract.MCPEnvironmentSources()
	sources[0] = "MUTATED"
	if !slices.Equal(antigravityContract.MCPEnvironmentSources(), []string{credential}) {
		t.Fatal("Antigravity locked environment sources exposed mutable canonical storage")
	}

	codexProject := mustTestMCPPlacement(t, aggregate.MCPPlacementCodexProject)
	codexProjectInput := testMCPProjectionInput(t, codexProject, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(codexProjectInput); err == nil ||
		!strings.Contains(err.Error(), "cannot lock credential dependencies") {
		t.Fatalf("Codex project credential error = %v", err)
	}

	openCodeProject := mustTestMCPPlacement(t, aggregate.MCPPlacementOpenCodeProject)
	openCodeProjectInput := testMCPProjectionInput(t, openCodeProject, []string{credential})
	if _, err := NewMCPProjectionSubjectContract(openCodeProjectInput); err == nil ||
		!strings.Contains(err.Error(), "cannot lock credential dependencies") {
		t.Fatalf("OpenCode project credential error = %v", err)
	}

	claudeInput.CredentialReferences = []string{credential, credential}
	if _, err := NewMCPProjectionSubjectContract(claudeInput); err == nil ||
		!strings.Contains(err.Error(), "duplicate MCP credential reference") {
		t.Fatalf("duplicate credential error = %v", err)
	}

	claudeInput.CredentialReferences = []string{"9TOKEN"}
	if _, err := NewMCPProjectionSubjectContract(claudeInput); err == nil ||
		!strings.Contains(err.Error(), "must not start with a digit") {
		t.Fatalf("invalid credential name error = %v", err)
	}

	realization, ok := antigravityContract.Realization()
	if !ok {
		t.Fatal("Antigravity contract is missing realization")
	}
	operations := make([]OperationContract, 0, len(antigravityContract.OperationKinds()))
	for _, operation := range antigravityContract.OperationKinds() {
		contract, present := antigravityContract.OperationContract(operation)
		if !present {
			t.Fatalf("Antigravity contract is missing operation %q", operation)
		}
		operations = append(operations, contract)
	}
	unknownSubject, err := topology.NewSubjectID(topology.SubjectProjection, "unknown.mcp", "context7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:              antigravityContract.EntityID(),
		SubjectID:             unknownSubject,
		Realization:           &realization,
		MCPEnvironmentSources: []string{credential},
		Ownership:             antigravityContract.Ownership(),
		OnAbsent:              antigravityContract.OnAbsent(),
		Replay:                antigravityContract.ReplayCoverage(),
		OperationContracts:    operations,
	})
	if err == nil || !strings.Contains(err.Error(), "require an implemented MCP placement") {
		t.Fatalf("unknown MCP subject error = %v", err)
	}
}

func TestMCPProjectionSubjectContractCorrelatesDelegatePlan(t *testing.T) {
	placement := mustTestMCPPlacement(t, aggregate.MCPPlacementClaudeProject)
	base := testMCPProjectionInput(t, placement, []string{"TOKEN"})
	plan := testDelegatePlan(t, "npx", []string{"-y", "@acme/mcp"}, []string{"TOKEN"})
	base.LauncherArgs = []string{"-y", "@acme/mcp"}
	base.DelegatePlan = &plan
	if _, err := NewMCPProjectionSubjectContract(base); err != nil {
		t.Fatalf("correlated delegate plan rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*MCPProjectionSubjectInput)
		want   string
	}{
		{name: "command", mutate: func(input *MCPProjectionSubjectInput) {
			input.LauncherCommand = "node"
			input.Graph = testMCPProjectionGraph(t, placement, "node", input.CredentialReferences)
		}, want: "delegate command"},
		{name: "args", mutate: func(input *MCPProjectionSubjectInput) { input.LauncherArgs = []string{"different"} }, want: "delegate args"},
		{name: "env", mutate: func(input *MCPProjectionSubjectInput) {
			input.CredentialReferences = nil
			input.Graph = testMCPProjectionGraph(t, placement, input.LauncherCommand, nil)
		}, want: "delegate env refs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			_, err := NewMCPProjectionSubjectContract(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func testMCPProjectionInput(
	t *testing.T,
	placement aggregate.MCPPlacement,
	credentials []string,
) MCPProjectionSubjectInput {
	t.Helper()
	input := MCPProjectionSubjectInput{
		Graph:                testMCPProjectionGraph(t, placement, "npx", credentials),
		EntityID:             mustContractEntityID(t, entity.KindMCPServer, "context7"),
		PlacementID:          placement.ID(),
		ServerID:             "context7",
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      "npx",
		CanonicalProjection:  `{"command":"npx"}`,
		CredentialReferences: append([]string(nil), credentials...),
	}
	if placement.Target() == target.TargetPi {
		contribution := testPiMCPProviderContribution(t, placement.Scope())
		reference := contribution.Reference()
		input.ProviderContribution = &reference
	}
	return input
}

func testMCPProjectionGraph(
	t *testing.T,
	placement aggregate.MCPPlacement,
	command string,
	credentials []string,
) topology.Graph {
	t.Helper()
	projection, err := topologymcp.ProjectionSubject(
		placement.Target(),
		placement.Scope(),
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := topologymcp.ExecutableSubject(command)
	if err != nil {
		t.Fatal(err)
	}
	subjects := []topology.SubjectID{projection, launcher}
	edges := []topology.Edge{topology.NewEdge(topology.EdgeLaunchesVia, projection, launcher)}
	for _, reference := range credentials {
		credential, err := topologymcp.EnvironmentReferenceSubject(reference)
		if err != nil {
			t.Fatal(err)
		}
		subjects = append(subjects, credential)
		edges = append(edges, topology.NewEdge(topology.EdgeDependsOn, projection, credential))
	}
	if placement.Target() == target.TargetPi {
		contribution := testPiMCPProviderContribution(t, placement.Scope())
		subjects = append(
			subjects,
			contribution.Provider().SubjectID(),
			contribution.SubjectID(),
		)
		edges = append(
			edges,
			topology.NewEdge(
				topology.EdgeProvidedBy,
				contribution.SubjectID(),
				contribution.Provider().SubjectID(),
			),
			topology.NewEdge(
				topology.EdgeBoundTo,
				contribution.SubjectID(),
				projection,
			),
		)
	}
	return mustTopologyGraph(t, subjects, edges)
}

func testPiMCPProviderContribution(
	t *testing.T,
	scope target.Scope,
) extensiontopology.Contribution {
	t.Helper()
	source := desiredtest.ExtensionSource(
		t,
		desiredextension.SourceKindHostSource,
		"npm:pi-mcp-adapter@^2.13.0",
	)
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := extensiontopology.NewContribution(
		carrier,
		extensiontopology.ContributionSpec{Kind: "mcp-client", Key: "default"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return contribution
}

func mustTopologyGraph(t *testing.T, subjects []topology.SubjectID, edges []topology.Edge) topology.Graph {
	t.Helper()
	graph, err := topology.NewGraph(subjects, edges)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func mustTestMCPPlacement(t *testing.T, id aggregate.MCPPlacementID) aggregate.MCPPlacement {
	t.Helper()
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		if placement.ID() == id {
			return placement
		}
	}
	t.Fatalf("MCP placement %q is missing", id)
	return aggregate.MCPPlacement{}
}

func testDelegatePlan(t *testing.T, commandName string, args []string, env []string) delegate.DelegatePlan {
	t.Helper()
	runner, err := delegate.NewRunner(delegate.RunnerPlain)
	if err != nil {
		t.Fatal(err)
	}
	command, err := delegate.NewCommandSpec(commandName, args)
	if err != nil {
		t.Fatal(err)
	}
	envValues := make(map[string]string, len(env))
	for _, name := range env {
		envValues[name] = name
	}
	envBindings := testDelegateEnvBindings(t, envValues)
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner: runner, Command: command, Env: envBindings,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestMCPProjectionSubjectContractUsesAggregateRealizationKind(t *testing.T) {
	contract, err := NewMCPProjectionSubjectContract(testMCPProjectionInput(
		t,
		mustTestMCPPlacement(t, aggregate.MCPPlacementCodexGlobal),
		nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := contract.Realization()
	if spec.Kind() != realization.RealizationManagedAggregateContribution {
		t.Fatalf("realization kind = %q", spec.Kind())
	}
}
