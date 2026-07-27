package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func SkillPathState(
	t *testing.T,
	name string,
	consumerValues []string,
	scopeValue string,
	path string,
	contentHash string,
) durable.ManagedPathState {
	t.Helper()
	consumers := parseTargets(t, consumerValues)
	scope := parseScope(t, scopeValue)
	placements, err := profile.ManagedPathPlacementsFor(entity.KindSkill, scope, consumers)
	if err != nil {
		t.Fatalf("ManagedPathPlacementsFor returned error: %v", err)
	}
	var placementID string
	for _, placement := range placements {
		if _, err := placement.ChildName(parseDestination(t, path)); err == nil {
			placementID = placement.ID()
			break
		}
	}
	if placementID == "" {
		t.Fatalf("no canonical Skill placement owns path %q", path)
	}
	return managedPathState(
		t,
		entity.KindSkill,
		name,
		placementID,
		consumers,
		scope,
		path,
		contentHash,
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
	)
}

func InstructionPathState(
	t *testing.T,
	name string,
	consumerValues []string,
	scopeValue string,
	path string,
	contentHash string,
) durable.ManagedPathState {
	t.Helper()
	consumers := parseTargets(t, consumerValues)
	scope := parseScope(t, scopeValue)
	var placement profile.SelectedManagedPathPlacement
	for index, consumer := range consumers {
		candidate, err := profile.ManagedFilePlacementFor(
			entity.KindInstructions,
			consumer,
			scope,
			parseDestination(t, path),
		)
		if err != nil {
			t.Fatalf("ManagedFilePlacementFor returned error: %v", err)
		}
		if index == 0 {
			placement = candidate
			continue
		}
		placement, err = profile.MergeManagedPathPlacements(placement, candidate)
		if err != nil {
			t.Fatalf("MergeManagedPathPlacements returned error: %v", err)
		}
	}
	return managedPathState(
		t,
		entity.KindInstructions,
		name,
		placement.ID(),
		consumers,
		scope,
		path,
		contentHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
	)
}

func Snapshot(t *testing.T, managedPaths ...durable.ManagedPathState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: managedPaths})
	if err != nil {
		t.Fatalf("durable.NewSnapshot returned error: %v", err)
	}
	return snapshot
}

func AssertSkillPathState(
	t *testing.T,
	snapshot durable.Snapshot,
	name string,
	selectedTarget string,
	scope string,
	path string,
	contentHash string,
) {
	t.Helper()
	AssertManagedPathState(
		t,
		snapshot,
		entity.KindSkill,
		name,
		[]string{selectedTarget},
		scope,
		path,
		contentHash,
		string(realization.PathProjectionDirectory),
	)
}

func AssertManagedPathState(
	t *testing.T,
	snapshot durable.Snapshot,
	kind entity.Kind,
	name string,
	consumerTargets []string,
	scope string,
	path string,
	contentHash string,
	contentKind string,
) {
	t.Helper()
	for _, state := range snapshot.ManagedPaths() {
		if string(state.Scope()) != scope || state.Destination().String() != path {
			continue
		}
		entityID, entityBacked := topologyprojection.EntityID(state.Subject())
		if !entityBacked || entityID.Kind() != kind || entityID.Name() != name {
			continue
		}
		actualTargets := make([]string, 0, len(state.ConsumerTargets()))
		for _, value := range state.ConsumerTargets() {
			actualTargets = append(actualTargets, string(value))
		}
		if !slices.Equal(actualTargets, consumerTargets) || string(state.ContentKind()) != contentKind {
			t.Fatalf("managed path state = %#v", state)
		}
		if string(state.ContentHash()) != contentHash {
			t.Fatalf("managed path state hash = %q, want %q", state.ContentHash(), contentHash)
		}
		return
	}
	t.Fatalf("managed path state %s:%s path=%q not found", kind, name, path)
}

func AssertSkillPathStateMissing(
	t *testing.T,
	snapshot durable.Snapshot,
	name string,
	scope string,
	path string,
) {
	t.Helper()
	AssertManagedPathStateMissing(t, snapshot, entity.KindSkill, name, scope, path)
}

func AssertManagedPathStateMissing(
	t *testing.T,
	snapshot durable.Snapshot,
	kind entity.Kind,
	name string,
	scope string,
	path string,
) {
	t.Helper()
	for _, state := range snapshot.ManagedPaths() {
		if string(state.Scope()) != scope || state.Destination().String() != path {
			continue
		}
		entityID, entityBacked := topologyprojection.EntityID(state.Subject())
		if entityBacked && entityID.Kind() == kind && entityID.Name() == name {
			t.Fatalf("managed %s path state unexpectedly found", kind)
		}
	}
}

func WriteStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("statefile.Marshal returned error: %v", err)
	}
	writeStatefileBytes(t, path, content)
}

// WriteUncheckedStatefile writes a deliberately malformed boundary fixture.
func WriteUncheckedStatefile(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent returned error: %v", err)
	}
	writeStatefileBytes(t, path, content)
}

// WriteStatefileWithInvalidManagedPathDestination corrupts one serialized v2
// managed-path destination for strict boundary rejection tests.
func WriteStatefileWithInvalidManagedPathDestination(
	t *testing.T,
	path string,
	snapshot durable.Snapshot,
	destination string,
) {
	t.Helper()
	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("statefile.Marshal returned error: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("json.Unmarshal statefile returned error: %v", err)
	}
	managedPaths, ok := document["managed_paths"].([]any)
	if !ok || len(managedPaths) != 1 {
		t.Fatalf("managed_paths = %#v, want exactly one row", document["managed_paths"])
	}
	row, ok := managedPaths[0].(map[string]any)
	if !ok {
		t.Fatalf("managed_paths[0] = %#v, want object", managedPaths[0])
	}
	row["destination"] = destination
	WriteUncheckedStatefile(t, path, document)
}

func AssertStateResource(
	t *testing.T,
	snapshot durable.Snapshot,
	selectedTarget string,
	path string,
	contentHash string,
) {
	t.Helper()
	AssertStateResourceNamed(t, snapshot, "project", selectedTarget, "project", path, contentHash)
}

func AssertStateResourceNamed(
	t *testing.T,
	snapshot durable.Snapshot,
	name string,
	selectedTarget string,
	scope string,
	path string,
	contentHash string,
) {
	t.Helper()
	AssertManagedPathState(
		t,
		snapshot,
		entity.KindInstructions,
		name,
		[]string{selectedTarget},
		scope,
		path,
		contentHash,
		string(realization.PathProjectionFile),
	)
}

func AssertStateResourceNamedKind(
	t *testing.T,
	snapshot durable.Snapshot,
	kind string,
	name string,
	selectedTarget string,
	scope string,
	path string,
	contentHash string,
) {
	t.Helper()
	if kind != string(entity.KindInstructions) {
		t.Fatalf("retired generic state assertion for kind %q", kind)
	}
	AssertStateResourceNamed(t, snapshot, name, selectedTarget, scope, path, contentHash)
}

func AssertStateResourceNamedKindContentPath(
	t *testing.T,
	snapshot durable.Snapshot,
	kind string,
	name string,
	selectedTarget string,
	scope string,
	path string,
	contentPath string,
	contentHash string,
) {
	t.Helper()
	if contentPath != "" {
		t.Fatalf("retired generic aggregate state assertion for content path %q", contentPath)
	}
	AssertStateResourceNamedKind(
		t,
		snapshot,
		kind,
		name,
		selectedTarget,
		scope,
		path,
		contentHash,
	)
}

func AssertStateResourceMissing(
	t *testing.T,
	snapshot durable.Snapshot,
	name string,
	selectedTarget string,
	scope string,
	path string,
) {
	t.Helper()
	AssertStateResourceMissingKind(
		t,
		snapshot,
		string(entity.KindInstructions),
		name,
		selectedTarget,
		scope,
		path,
	)
}

func AssertStateResourceMissingKind(
	t *testing.T,
	snapshot durable.Snapshot,
	kind string,
	name string,
	_ string,
	scope string,
	path string,
) {
	t.Helper()
	if kind != string(entity.KindInstructions) {
		t.Fatalf("retired generic state assertion for kind %q", kind)
	}
	AssertManagedPathStateMissing(t, snapshot, entity.KindInstructions, name, scope, path)
}

func managedPathState(
	t *testing.T,
	kind entity.Kind,
	name string,
	placementID string,
	consumers []target.Target,
	scope target.Scope,
	path string,
	contentHash string,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
) durable.ManagedPathState {
	t.Helper()
	// CLI fixtures may use a semantic seed; malformed persistence tests mutate
	// encoded bytes instead of bypassing the canonical state constructor.
	canonicalHash := artifact.ContentHash(contentHash)
	if canonicalHash.Validate() != nil {
		canonicalHash = artifact.HashFileContent([]byte(contentHash))
	}
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subject, err := topologyprojection.Subject(id, placementID)
	if err != nil {
		t.Fatalf("projection.Subject returned error: %v", err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		consumers,
		scope,
		parseDestination(t, path),
		canonicalHash,
		contentKind,
		permissionPolicy,
		0,
	)
	if err != nil {
		t.Fatalf("durable.NewManagedPathState returned error: %v", err)
	}
	return state
}

func parseTargets(t *testing.T, values []string) []target.Target {
	t.Helper()
	result := make([]target.Target, 0, len(values))
	for _, value := range values {
		parsed, err := target.ParseTarget(value)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", value, err)
		}
		result = append(result, parsed)
	}
	return result
}

func parseScope(t *testing.T, value string) target.Scope {
	t.Helper()
	scope, err := target.ParseScope(value)
	if err != nil {
		t.Fatalf("ParseScope(%q): %v", value, err)
	}
	return scope
}

func writeStatefileBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
