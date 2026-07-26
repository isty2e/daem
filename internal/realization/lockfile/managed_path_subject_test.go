package lockfile

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestMarshalAndLoadManagedPathProjection(t *testing.T) {
	supply := directSkillSubjectContract(t, "oracle")
	projection := managedPathSkillSubjectContract(
		t,
		"oracle",
		[]target.Target{target.TargetCodex, target.TargetAntigravityCLI},
	)
	file := lockfileWithSubjects(t, supply, projection)

	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	for _, fragment := range []string{
		`consumer_targets = ["antigravity-cli", "codex"]`,
		`placement_mode = "copy"`,
		`content_kind = "directory"`,
		`permission_policy = "none"`,
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("managed path lockfile is missing %q:\n%s", fragment, rendered)
		}
	}

	loaded, err := Load(writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
}

func TestLoadRejectsNonCanonicalManagedPathConsumers(t *testing.T) {
	file := lockfileWithSubjects(
		t,
		directSkillSubjectContract(t, "oracle"),
		managedPathSkillSubjectContract(
			t,
			"oracle",
			[]target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		),
	)
	canonical := marshalLockfileForTest(t, file)

	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "reverse canonical order", content: reverseFirstStringArrayField(t, canonical, "consumer_targets")},
		{name: "duplicate consumer", content: duplicateFirstStringArrayFieldValue(t, canonical, "consumer_targets")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeLockfileText(t, test.content))
			if err == nil || !strings.Contains(err.Error(), "non-canonical values") {
				t.Fatalf("Load error = %v, want non-canonical value rejection", err)
			}
		})
	}
}

func TestManagedPathRealizationCodecRoundTripsPortableDataRootAndExactMode(t *testing.T) {
	for _, fileMode := range []os.FileMode{0, 0o640, 0o700, 0o750} {
		t.Run(fileMode.String(), func(t *testing.T) {
			realization := exactManagedPathRealization(t, fileMode)
			dto := realizationToDTO(realization, true)
			if dto.ManagedPath.ExactPermissionMode == nil ||
				*dto.ManagedPath.ExactPermissionMode != uint32(fileMode) {
				t.Fatalf("encoded exact permission mode = %#v, want %04o", dto.ManagedPath.ExactPermissionMode, fileMode)
			}

			roundTripped, err := realizationFromDTO(dto)
			if err != nil {
				t.Fatal(err)
			}
			if roundTripped == nil || !roundTripped.Equal(realization) {
				t.Fatalf("round-tripped realization = %#v, want %#v", roundTripped, realization)
			}
			projection, ok := roundTripped.ManagedPathProjection()
			exactMode, exact := projection.ExactPermissionMode()
			if !ok || projection.Destination() != "@data/hook-assets/guard/sha256-deadbeef/asset" ||
				!exact || exactMode.FileMode() != fileMode {
				t.Fatalf("round-tripped projection = %#v, %t", projection, ok)
			}
		})
	}
}

func TestManagedPathRealizationTOMLPreservesExplicitExactModeZero(t *testing.T) {
	dto := realizationToDTO(exactManagedPathRealization(t, 0), true)

	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(dto.ManagedPath); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), "exact_permission_mode = 0") {
		t.Fatalf("encoded managed path omitted explicit exact mode zero:\n%s", encoded.String())
	}

	var decoded managedPathProjectionDTO
	if _, err := toml.Decode(encoded.String(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ExactPermissionMode == nil || *decoded.ExactPermissionMode != 0 {
		t.Fatalf("decoded exact mode = %#v, want explicit zero", decoded.ExactPermissionMode)
	}
}

func TestManagedPathRealizationTOMLRejectsMalformedExactModes(t *testing.T) {
	for _, value := range []string{"-1", "4294967296", `"0700"`, "1.5"} {
		t.Run(value, func(t *testing.T) {
			var decoded managedPathProjectionDTO
			if _, err := toml.Decode("exact_permission_mode = "+value, &decoded); err == nil {
				t.Fatalf("TOML decoder accepted exact_permission_mode = %s", value)
			}
		})
	}
}

func TestManagedPathRealizationCodecRejectsMissingAndUnknownPermissionPolicy(t *testing.T) {
	spec := exactManagedPathRealization(t, 0o700)

	for _, policy := range []string{"", "future"} {
		dto := realizationToDTO(spec, true)
		dto.ManagedPath.PermissionPolicy = policy
		if _, err := realizationFromDTO(dto); err == nil || !strings.Contains(err.Error(), "permission policy") {
			t.Fatalf("permission policy %q error = %v, want strict codec rejection", policy, err)
		}
	}
}

func TestManagedPathRealizationCodecRejectsExactModeContradictions(t *testing.T) {
	spec := exactManagedPathRealization(t, 0o700)
	for _, test := range []struct {
		name   string
		mutate func(*managedPathProjectionDTO)
		want   string
	}{
		{
			name:   "missing exact mode",
			mutate: func(dto *managedPathProjectionDTO) { dto.ExactPermissionMode = nil },
			want:   "exact path permission mode is required",
		},
		{
			name: "type bits",
			mutate: func(dto *managedPathProjectionDTO) {
				mode := uint32(os.ModeSetuid | 0o700)
				dto.ExactPermissionMode = &mode
			},
			want: "permission bits only",
		},
		{
			name: "non-exact policy with exact mode",
			mutate: func(dto *managedPathProjectionDTO) {
				dto.PermissionPolicy = string(realization.PathPermissionsExecutableClass)
			},
			want: "must not carry",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dto := realizationToDTO(spec, true)
			test.mutate(dto.ManagedPath)
			if _, err := realizationFromDTO(dto); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("realizationFromDTO error = %v, want %q", err, test.want)
			}
		})
	}
}

func exactManagedPathRealization(t *testing.T, fileMode os.FileMode) realization.RealizationSpec {
	t.Helper()
	exactMode, err := realization.NewExactPathPermissionMode(fileMode)
	if err != nil {
		t.Fatal(err)
	}
	realization, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID:            "hookasset.global.store",
		ConsumerTargets:        []target.Target{target.TargetCodex, target.TargetClaudeCode},
		Scope:                  target.ScopeGlobal,
		Destination:            "@data/hook-assets/guard/sha256-deadbeef/asset",
		ContentKind:            realization.PathProjectionFile,
		PlacementMode:          realization.PathProjectionCopy,
		PermissionPolicy:       realization.PathPermissionsExact,
		ExactPermissionMode:    exactMode,
		AdapterContractVersion: "hookasset-file-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return realization
}

func managedPathSkillSubjectContract(
	t *testing.T,
	name string,
	consumers []target.Target,
) lock.LockedSubjectContract {
	t.Helper()
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		target.ScopeProject,
		consumers,
	)
	if err != nil {
		t.Fatalf("ManagedPathPlacementsFor returned error: %v", err)
	}
	if len(placements) != 1 {
		t.Fatalf("managed path placements = %d, want one shared occupancy", len(placements))
	}
	placement := placements[0]
	destination, err := placement.ChildDestination(name)
	if err != nil {
		t.Fatalf("ChildDestination returned error: %v", err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatalf("write route: %v", err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatalf("remove route: %v", err)
	}
	spec, err := placement.Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatalf("Realize returned error: %v", err)
	}
	entityID := desiredEntityID(t, entity.KindSkill, name)
	subjectID, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatalf("projection subject: %v", err)
	}
	contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID:      entityID,
		SubjectID:     subjectID,
		Realization:   spec,
		WriteRouteID:  writeRoute.RouteID(),
		RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatalf("NewManagedPathSubjectContract returned error: %v", err)
	}
	return contract
}
