package clipresent_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func TestPrintDeltaSummaryReportsResolvedRefChanges(t *testing.T) {
	sourceID := artifact.SourceID("git:locator=https%3A%2F%2Fexample.test%2Frepo.git&path=skills%2Foracle&ref=name%3Amain")
	before := snapshottest.File(t, testExactSupplyContract(t, entity.KindSkill, "oracle", sourceID, "old", "old"))
	after := snapshottest.File(t, testExactSupplyContract(t, entity.KindSkill, "oracle", sourceID, "new", "new"))

	var stdout bytes.Buffer
	clipresent.PrintDeltaSummaryWithOptions(&stdout, lock.BuildDelta(before, after), clipresent.HumanOptions{Verbose: true})

	for _, want := range []string{
		"lockfile changes: added=0 changed=1 removed=0 unchanged=0",
		`resource/skill/oracle changed=exact_supply,derivation`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestPrintJSONWritesStableLockProjection(t *testing.T) {
	before := snapshottest.File(
		t,
		testExactSupplyContract(t, entity.KindSkill, "oracle", "local:skills/oracle?mode=vendor", "", "old"),
	)
	afterSubjects := []lock.LockedSubjectContract{
		testExactSupplyContract(t, entity.KindSkill, "oracle", "local:skills/oracle?mode=vendor", "", "new"),
		testExactSupplyContract(t, entity.KindSkill, "review", "local:skills/review?mode=vendor", "", "review"),
	}
	afterSubjects = append(afterSubjects, testInstructionsContracts(t, "project", "AGENTS.md", "project")...)
	after := snapshottest.File(t, afterSubjects...)

	var stdout bytes.Buffer
	err := clipresent.PrintJSON(&stdout, clipresent.JSONInput{
		Command:       "lock",
		Mode:          "dry-run",
		ManifestPath:  "/repo/daem.toml",
		LockfilePath:  "/repo/daem.lock.toml",
		PreviousFound: true,
		Lockfile:      after,
		Delta:         lock.BuildDelta(before, after),
	})
	if err != nil {
		t.Fatalf("PrintJSON returned error: %v", err)
	}

	want := fmt.Sprintf(`{
  "schema_version": %d,
  "command": "lock",
  "mode": "dry-run",
  "manifest_path": "/repo/daem.toml",
  "lockfile_path": "/repo/daem.lock.toml",
  "previous_found": true,
  "entry_counts": {
    "subjects": 4,
    "order_constraints": 0
  },
  "change_counts": {
    "added": 3,
    "changed": 1,
    "removed": 0,
    "unchanged": 0
  },
  "order_change_counts": {
    "added": 0,
    "changed": 0,
    "removed": 0,
    "unchanged": 0
  },
  "has_changes": true,
  "subject_changes": [
    {
      "status": "added",
      "subject": {
        "kind": "projection",
        "namespace": "instructions.project.agents",
        "name": "instructions:project"
      },
      "after": {
        "entity_id": "instructions:project",
        "subject": {
          "kind": "projection",
          "namespace": "instructions.project.agents",
          "name": "instructions:project"
        },
        "ownership": "manifest",
        "on_absent": "apply",
        "realization": {
          "kind": "managed_path_projection",
          "placement_id": "instructions.project.agents",
          "consumer_targets": [
            "codex"
          ],
          "scope": "project",
          "destination": "AGENTS.md",
          "content_kind": "file",
          "placement_mode": "copy",
          "permission_policy": "executable-class",
          "adapter_contract_version": "managed-instruction-file-v1"
        },
        "operations": [
          "remove_projection",
          "write_projection"
        ]
      }
    },
    {
      "status": "added",
      "subject": {
        "kind": "resource",
        "namespace": "instructions",
        "name": "project"
      },
      "after": {
        "entity_id": "instructions:project",
        "subject": {
          "kind": "resource",
          "namespace": "instructions",
          "name": "project"
        },
        "ownership": "manifest",
        "on_absent": "apply",
        "exact_supply": {
          "source_id": "local:AGENTS.md?mode=vendor",
          "kind": "file",
          "content_hash": "sha256:3beab0b070db64ccb7c76c4f3e353e2ae87425943ed51da24836dfc514d8818f"
        },
        "exact_file_use": {
          "scope": "project",
          "executable": false
        },
        "operations": [
          "observe",
          "write_projection"
        ]
      }
    },
    {
      "status": "changed",
      "subject": {
        "kind": "resource",
        "namespace": "skill",
        "name": "oracle"
      },
      "before": {
        "entity_id": "skill:oracle",
        "subject": {
          "kind": "resource",
          "namespace": "skill",
          "name": "oracle"
        },
        "ownership": "manifest",
        "on_absent": "apply",
        "exact_supply": {
          "source_id": "local:skills/oracle?mode=vendor",
          "kind": "directory",
          "content_hash": "sha256:e260df74f455a1752102e3dfe0ccffa64a99b3d159ab2c0d0b2658751b0bcc75"
        },
        "operations": [
          "observe",
          "write_projection"
        ]
      },
      "after": {
        "entity_id": "skill:oracle",
        "subject": {
          "kind": "resource",
          "namespace": "skill",
          "name": "oracle"
        },
        "ownership": "manifest",
        "on_absent": "apply",
        "exact_supply": {
          "source_id": "local:skills/oracle?mode=vendor",
          "kind": "directory",
          "content_hash": "sha256:a839fdeee49a71484bd8a852dd8b15a898d23547ded6358cb2303572d801a511"
        },
        "operations": [
          "observe",
          "write_projection"
        ]
      }
    },
    {
      "status": "added",
      "subject": {
        "kind": "resource",
        "namespace": "skill",
        "name": "review"
      },
      "after": {
        "entity_id": "skill:review",
        "subject": {
          "kind": "resource",
          "namespace": "skill",
          "name": "review"
        },
        "ownership": "manifest",
        "on_absent": "apply",
        "exact_supply": {
          "source_id": "local:skills/review?mode=vendor",
          "kind": "directory",
          "content_hash": "sha256:6b735f9f737cb8ad36c94be31b223faadb13f43eed5da1c4f7abede57f683c13"
        },
        "operations": [
          "observe",
          "write_projection"
        ]
      }
    }
  ],
  "order_constraint_changes": []
}
`, contractversion.LockComparisonJSON)
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestPrintDryRunSummaryReportsRepairedLocks(t *testing.T) {
	file := snapshottest.File(t, testRepairedSkillContract(t))

	var stdout bytes.Buffer
	clipresent.PrintDryRunSummaryWithOptions(&stdout, clipresent.DryRunSummaryInput{
		LockfilePath: "/repo/daem.lock.toml",
		Lockfile:     file,
		Delta:        lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, file),
		NextCommand:  "daem lock --manifest /repo/daem.toml",
	}, clipresent.HumanOptions{Verbose: true})

	for _, want := range []string{
		"would write lockfile: /repo/daem.lock.toml",
		"lockfile entries: subjects=1",
		"lockfile changes: added=1 changed=0 removed=0 unchanged=0",
		"lockfile.subject.added:\n  - resource/skill/oracle",
		"locked.subject:\n  - resource/skill/oracle (repaired)",
		"repaired resources: 1",
		"next: run daem lock --manifest /repo/daem.toml",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestPrintJSONIncludesMCPSubjectChanges(t *testing.T) {
	record := testMCPSubjectRecord(t, "context7")
	after := snapshottest.File(t, record)

	var stdout bytes.Buffer
	err := clipresent.PrintJSON(&stdout, clipresent.JSONInput{
		Command:       "lock",
		Mode:          "dry-run",
		ManifestPath:  "/repo/daem.toml",
		LockfilePath:  "/repo/daem.lock.toml",
		PreviousFound: false,
		Lockfile:      after,
		Delta:         lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, after),
	})
	if err != nil {
		t.Fatalf("PrintJSON returned error: %v", err)
	}

	var payload struct {
		EntryCounts struct {
			Subjects int `json:"subjects"`
		} `json:"entry_counts"`
		ChangeCounts struct {
			Added int `json:"added"`
		} `json:"change_counts"`
		SubjectChanges []struct {
			Status  string `json:"status"`
			Subject struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			After struct {
				EntityID    string `json:"entity_id"`
				Ownership   string `json:"ownership"`
				OnAbsent    string `json:"on_absent"`
				Realization struct {
					Kind                   string `json:"kind"`
					Target                 string `json:"target"`
					Scope                  string `json:"scope"`
					AggregateRoot          string `json:"aggregate_root"`
					ContentPath            string `json:"content_path"`
					AdapterContractVersion string `json:"adapter_contract_version"`
				} `json:"realization"`
			} `json:"after"`
		} `json:"subject_changes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode lock json: %v", err)
	}
	if payload.EntryCounts.Subjects != 1 || payload.ChangeCounts.Added != 1 {
		t.Fatalf("counts = %#v/%#v", payload.EntryCounts, payload.ChangeCounts)
	}
	if len(payload.SubjectChanges) != 1 {
		t.Fatalf("subject_changes = %#v, want one", payload.SubjectChanges)
	}
	change := payload.SubjectChanges[0]
	if change.Status != "added" ||
		change.Subject.Kind != "projection" ||
		change.Subject.Namespace != "claude-code.project.mcp-server" ||
		change.Subject.Name != "context7" ||
		change.After.EntityID != "mcp_server:context7" ||
		change.After.Ownership != "manifest" ||
		change.After.OnAbsent != "remove_binding" ||
		change.After.Realization.Kind != string(realization.RealizationManagedAggregateContribution) ||
		change.After.Realization.Target != string(target.TargetClaudeCode) ||
		change.After.Realization.Scope != string(target.ScopeProject) ||
		change.After.Realization.AggregateRoot != aggregate.ClaudeProjectMCPConfigPath ||
		change.After.Realization.ContentPath != mcpcodec.ClaudeProjectMCPContentPath("context7") ||
		change.After.Realization.AdapterContractVersion != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf("subject change = %#v", change)
	}
	if strings.Contains(stdout.String(), "canonical_projection") ||
		strings.Contains(stdout.String(), "@upstash/context7-mcp") {
		t.Fatalf("lock json leaked canonical projection body: %s", stdout.String())
	}
}

func TestPrintDeltaSummaryIncludesMCPSubjectChanges(t *testing.T) {
	record := testMCPSubjectRecord(t, "context7")
	after := snapshottest.File(t, record)

	var stdout bytes.Buffer
	clipresent.PrintDeltaSummaryWithOptions(&stdout, lock.BuildDelta(lock.File{Version: lock.CurrentVersion}, after), clipresent.HumanOptions{Verbose: true})

	for _, want := range []string{
		"lockfile changes: added=1 changed=0 removed=0 unchanged=0",
		"lockfile.subject.added:",
		"projection/claude-code.project.mcp-server/context7",
		`target="claude-code" scope="project" aggregate_root=".mcp.json" content_path="/mcpServers/context7"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "@upstash/context7-mcp") {
		t.Fatalf("lock summary leaked canonical projection body: %s", stdout.String())
	}
}

func testExactSupplyContract(
	t *testing.T,
	kind entity.Kind,
	name string,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	content string,
) lock.LockedSubjectContract {
	t.Helper()
	artifactKind := artifact.ArtifactKindDirectory
	var exactFileUse *lock.ExactFileUse
	if kind == entity.KindInstructions {
		artifactKind = artifact.ArtifactKindFile
		fileUse, err := lock.NewExactFileUse(target.ScopeProject, false)
		if err != nil {
			t.Fatalf("lock.NewExactFileUse returned error: %v", err)
		}
		exactFileUse = &fileUse
	}
	return snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         kind,
		Name:         name,
		SourceID:     sourceID,
		ResolvedRef:  resolvedRef,
		ArtifactKind: artifactKind,
		ContentHash:  artifact.HashFileContent([]byte(content)),
		ExactFileUse: exactFileUse,
	})
}

func testInstructionsContracts(
	t *testing.T,
	name string,
	sourcePath string,
	content string,
) []lock.LockedSubjectContract {
	t.Helper()
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatalf("lock.NewExactFileUse returned error: %v", err)
	}
	supply := snapshottest.ExactSupplyContract(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindInstructions,
		Name:         name,
		SourceID:     artifact.SourceID("local:" + sourcePath + "?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindFile,
		ContentHash:  artifact.HashFileContent([]byte(content)),
		ExactFileUse: &fileUse,
	})
	value := desiredtest.Instructions(t, instructions.Spec{
		Name:    name,
		Source:  sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
	projections, err := refine.InstructionsPathProjections(value)
	if err != nil {
		t.Fatalf("InstructionsPathProjections returned error: %v", err)
	}
	return append([]lock.LockedSubjectContract{supply}, projections...)
}

func testRepairedSkillContract(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	sourceID := artifact.SourceID("local:skills/oracle?mode=vendor")
	input := testExactIdentity(t, sourceID, artifact.HashFileContent([]byte("original tree")))
	output := testExactIdentity(t, sourceID, artifact.HashFileContent([]byte("repaired tree")))
	oldBytes := []byte("description: Demo\n")
	newBytes := []byte("name: oracle\ndescription: Demo\n")
	operation, err := skillrepair.NewReplaceBytesOperation(
		"SKILL.md",
		0,
		oldBytes,
		newBytes,
		artifact.HashFileContent(oldBytes),
		artifact.HashFileContent(newBytes),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation returned error: %v", err)
	}
	recipe, err := skillrepair.NewRecipe(input, output, []skillrepair.Operation{operation})
	if err != nil {
		t.Fatalf("NewRecipe returned error: %v", err)
	}
	derivation, err := lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
		InputIdentity:          input,
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       fmt.Sprintf("v%d", recipe.Version()),
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: output,
	})
	if err != nil {
		t.Fatalf("NewDeterministicTransformDerivation returned error: %v", err)
	}
	entityID, err := entity.New(entity.KindSkill, "oracle")
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  output,
		Derivation:   derivation,
		RepairRecipe: &recipe,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}
	return contract
}

func testExactIdentity(
	t *testing.T,
	sourceID artifact.SourceID,
	contentHash artifact.ContentHash,
) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(sourceID, "", artifact.ArtifactKindDirectory, contentHash)
	if err != nil {
		t.Fatalf("artifact.NewExactIdentity returned error: %v", err)
	}
	return identity
}

func testMCPSubjectRecord(t *testing.T, serverID string) lock.LockedSubjectContract {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeProject,
		ServerID:            serverID,
		LauncherCommand:     "npx",
		LauncherArgs:        []string{"-y", "@upstash/context7-mcp"},
		CanonicalProjection: string(canonical),
	})
}
