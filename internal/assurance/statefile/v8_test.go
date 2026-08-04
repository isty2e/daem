package statefile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestSnapshotV8GoldenShapeAndSemanticRoundTrip(t *testing.T) {
	snapshot := testV8Snapshot(t)
	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"version": 8`,
		`"statefile_authority": {`,
		`"semantics_witness": "exact-v1:"`,
		`"managed_paths": [`,
		`"managed_aggregate_contributions": [`,
		`"pending_carrier_installs": [`,
		`"pending_carrier_removals": [`,
		`"managed_carrier_claims": [`,
		`"carrier_subject": {`,
		`"install_request": {`,
		`"delegate_attempts": [`,
		`"host_route_attempts": [`,
		`"operation": "remove"`,
		`"effect_postconditions": [`,
		`"effect_baselines": [`,
	} {
		if !strings.Contains(string(content), fragment) {
			t.Fatalf("v8 content missing %q:\n%s", fragment, content)
		}
	}
	for _, forbidden := range []string{
		`"resources"`,
		`"managed":`,
		`"host_relation_authorities":`,
		`"grants_apply_skip_authority"`,
		`"runtime"`,
		`"readiness"`,
	} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("v8 content contains forbidden field %q:\n%s", forbidden, content)
		}
	}
	decoded, err := Decode(content)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Equal(decoded) {
		t.Fatal("decoded snapshot differs from encoded snapshot")
	}
	reencoded, err := Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(reencoded) {
		t.Fatalf("encoding is not deterministic:\n%s\n---\n%s", content, reencoded)
	}
}

func TestSnapshotV8RejectsMissingOrUnknownPathAuthorityWitness(t *testing.T) {
	content, err := Marshal(testV8Snapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		witness string
		want    string
	}{
		{name: "missing", want: "semantics witness is required"},
		{name: "unknown", witness: "future-v1:", want: "unsupported path authority semantics witness"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var persisted snapshotDTO
			if err := json.Unmarshal(content, &persisted); err != nil {
				t.Fatal(err)
			}
			persisted.ManagedCarrierClaims[0].Owner.StatefileAuthority.Witness = test.witness
			mutated, err := json.Marshal(persisted)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(mutated); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSnapshotV8RoundTripsExplicitAdoptionProvenanceWithoutVersionChange(t *testing.T) {
	snapshot := testV8Snapshot(t)
	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	adoptedContent := strings.ReplaceAll(
		string(content),
		string(durablecarrier.ClaimProvenanceInstalledObserved),
		string(durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved),
	)
	if adoptedContent == string(content) {
		t.Fatal("fixture contains no carrier claim provenance")
	}
	if !strings.Contains(adoptedContent, `"version": 8`) {
		t.Fatal("adopted claim changed statefile schema version")
	}

	decoded, err := Decode([]byte(adoptedContent))
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range decoded.ManagedCarrierClaims() {
		if claim.Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
			t.Fatalf("decoded provenance = %q, want explicit adoption", claim.Provenance())
		}
	}
	for _, pending := range decoded.PendingCarrierRemovals() {
		if pending.Claim().Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
			t.Fatalf("pending removal claim provenance = %q, want explicit adoption", pending.Claim().Provenance())
		}
	}
	reencoded, err := Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != adoptedContent {
		t.Fatalf("adopted provenance encoding is not deterministic:\n%s\n---\n%s", adoptedContent, reencoded)
	}
}

func TestSnapshotV8RoundTripsPreEffectContentBaseline(t *testing.T) {
	snapshot := testV8Snapshot(t)
	claim := snapshot.ManagedCarrierClaims()[0]
	originalPending := snapshot.PendingCarrierRemovals()[0]
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.LocalSourceUnchanged},
	)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := durablecarrier.NewContentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
		artifact.HashFileContent([]byte("local-before")),
	)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet([]durablecarrier.EffectBaseline{baseline})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		originalPending.RemoveRequest(),
		requirements,
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = snapshot.WithPendingCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
	)
	if err != nil {
		t.Fatal(err)
	}

	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(content)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Equal(decoded) {
		t.Fatalf("pre-effect baseline round trip changed snapshot:\n%s", content)
	}
}

func TestSnapshotV8EmptyEncodingIsCanonical(t *testing.T) {
	content, err := Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 8,
  "managed_paths": [],
  "managed_aggregate_contributions": [],
  "pending_carrier_installs": [],
  "pending_carrier_removals": [],
  "managed_carrier_claims": [],
  "delegate_attempts": [],
  "host_route_attempts": []
}`
	if string(content) != want {
		t.Fatalf("empty encoding:\n%s\nwant:\n%s", content, want)
	}
}

func TestSnapshotV8MarshalRejectsMalformedManagedAggregateContribution(t *testing.T) {
	const secretCanary = "STATEFILE_SECRET_CANARY"

	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	contribution, err := placement.Contribution(
		`{"event":"Stop","group":{"hooks":[{"type":"command","command":"` +
			secretCanary + `","unexpected":true}]}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	subject := testV8ProjectionSubject(t, entity.KindHook, "guard", string(placement.ID()))
	state, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedAggregates: []durable.ManagedAggregateState{state},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = Marshal(snapshot)
	if err == nil ||
		!strings.Contains(err.Error(), "managed_aggregate_contributions[0]") ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Marshal error = %v, want typed aggregate codec rejection", err)
	}
	if strings.Contains(err.Error(), secretCanary) {
		t.Fatalf("Marshal leaked contribution secret canary: %q", err)
	}
}

func TestSnapshotV8MarshalRejectsHookSetBeyondProjectionCardinality(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude Code project Hook placement is missing")
	}
	states := make([]durable.ManagedAggregateState, 0, hookdocument.MaximumEvents+1)
	for index := range hookdocument.MaximumEvents + 1 {
		canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
			Event: fmt.Sprintf("Event%03d", index), Type: "command", Command: "true",
		})
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := placement.Contribution(canonical)
		if err != nil {
			t.Fatal(err)
		}
		subject := testV8ProjectionSubject(
			t,
			entity.KindHook,
			fmt.Sprintf("event-%03d", index),
			string(placement.ID()),
		)
		state, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: states})
	if err != nil {
		t.Fatal(err)
	}

	_, err = Marshal(snapshot)
	if !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
		t.Fatalf("Marshal error = %v, want Hook structural budget error", err)
	}
}

func TestSnapshotV8RejectsOldUnknownDuplicateAndTrailingJSON(t *testing.T) {
	base := `{"version":8,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "v1",
			content: `{"version":1,"resources":[]}`,
			want:    "unsupported statefile version 1",
		},
		{
			name:    "v2",
			content: strings.Replace(base, `"version":8`, `"version":2`, 1),
			want:    "unsupported statefile version 2",
		},
		{
			name:    "v3",
			content: strings.Replace(base, `"version":8`, `"version":3`, 1),
			want:    "unsupported statefile version 3",
		},
		{
			name:    "v4",
			content: strings.Replace(base, `"version":8`, `"version":4`, 1),
			want:    "unsupported statefile version 4",
		},
		{
			name:    "v5",
			content: strings.Replace(base, `"version":8`, `"version":5`, 1),
			want:    "unsupported statefile version 5",
		},
		{
			name:    "v6",
			content: strings.Replace(base, `"version":8`, `"version":6`, 1),
			want:    "unsupported statefile version 6",
		},
		{
			name:    "legacy v7",
			content: strings.Replace(base, `"version":8`, `"version":7`, 1),
			want:    "use the daem version that wrote it",
		},
		{
			name:    "unknown version",
			content: strings.Replace(base, `"version":8`, `"version":9`, 1),
			want:    "written by a newer daem",
		},
		{
			name:    "future version before strict schema",
			content: `{"version":9,"future":true}`,
			want:    "written by a newer daem",
		},
		{
			name:    "unknown field",
			content: strings.Replace(base, `"version":8`, `"version":8,"ready":true`, 1),
			want:    "unknown field",
		},
		{
			name:    "duplicate key",
			content: strings.Replace(base, `"version":8`, `"version":8,"version":8`, 1),
			want:    "duplicate object key",
		},
		{
			name:    "nested duplicate key",
			content: `{"version":8,"managed_paths":[{"subject":{"kind":"projection","kind":"projection","namespace":"x","name":"y"}}],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`,
			want:    "duplicate object key",
		},
		{
			name:    "trailing value",
			content: base + `{}`,
			want:    "multiple JSON values",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadAdmitsOnlyExactEmptyRetiredV7Statefile(t *testing.T) {
	const retiredEmpty = `{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(retiredEmpty), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(t.Context(), path)
	if err != nil {
		t.Fatalf("Load exact empty retired v7: %v", err)
	}
	if !snapshot.Equal(durable.EmptySnapshot()) {
		t.Fatalf("Load exact empty retired v7 = %#v, want canonical empty", snapshot)
	}
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != retiredEmpty {
		t.Fatalf("read-only Load rewrote retired statefile:\n%s", retained)
	}
	if _, err := Decode([]byte(retiredEmpty)); err == nil ||
		!strings.Contains(err.Error(), "unsupported statefile version 7") {
		t.Fatalf("strict Decode error = %v, want retired-version rejection", err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing family",
			content: `{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[]}`,
			want:    "exact empty retired statefile",
		},
		{
			name:    "null family",
			content: strings.Replace(retiredEmpty, `"managed_paths":[]`, `"managed_paths":null`, 1),
			want:    "exact empty retired statefile",
		},
		{
			name:    "populated family",
			content: strings.Replace(retiredEmpty, `"managed_paths":[]`, `"managed_paths":[{"path":"legacy"}]`, 1),
			want:    "use the daem version that wrote it",
		},
		{
			name:    "unknown top-level field",
			content: strings.Replace(retiredEmpty, `"version":7`, `"version":7,"future":true`, 1),
			want:    "unknown field",
		},
		{
			name: "case-variant empty override",
			content: strings.Replace(
				retiredEmpty,
				`"managed_paths":[]`,
				`"managed_paths":[{"path":"legacy"}],"MANAGED_PATHS":[]`,
				1,
			),
			want: "ASCII lower_snake_case",
		},
		{
			name:    "case-variant version alias",
			content: strings.Replace(retiredEmpty, `"version":7`, `"Version":7`, 1),
			want:    "ASCII lower_snake_case",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(t.Context(), path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSnapshotV8RejectsInvalidUTF8(t *testing.T) {
	content := []byte(`{"version":8,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`)
	content = append(content[:len(content)-1], 0xff, '}')
	if _, err := Decode(content); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("Decode error = %v, want invalid UTF-8 rejection", err)
	}
}

func TestLoadRejectsOversizedStatefile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((16 << 20) + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(t.Context(), path); err == nil || !strings.Contains(err.Error(), "exceeds 16777216 bytes") {
		t.Fatalf("Load error = %v, want bounded statefile rejection", err)
	}
}

func TestDecodeRejectsOversizedStatefile(t *testing.T) {
	content := make([]byte, (16<<20)+1)
	for index := range content {
		content[index] = ' '
	}
	if _, err := Decode(content); err == nil || !strings.Contains(err.Error(), "exceeds 16777216 bytes") {
		t.Fatalf("Decode error = %v, want bounded statefile rejection", err)
	}
}

func TestSnapshotV8RejectsForgedManagedPathOccupancy(t *testing.T) {
	snapshot := testV8Snapshot(t)
	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(
		string(content),
		`"namespace": "skill.project.agents"`,
		`"namespace": "skill.project.forged"`,
		1,
	)
	if forged == string(content) {
		t.Fatal("fixture replacement did not change content")
	}
	if _, err := Decode([]byte(forged)); err == nil ||
		!strings.Contains(err.Error(), "not selected by its consumers") {
		t.Fatalf("Decode error = %v, want forged placement rejection", err)
	}

	subject := testV8ProjectionSubject(t, entity.KindSkill, "oracle", "skill.project.forged")
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.HashFileContent([]byte("oracle")),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	forgedSnapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{state},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Marshal(forgedSnapshot); err == nil ||
		!strings.Contains(err.Error(), "not selected by its consumers") {
		t.Fatalf("Marshal error = %v, want forged placement rejection", err)
	}
}

func TestSnapshotV8RejectsNonCanonicalAndContradictoryRows(t *testing.T) {
	snapshot := testV8Snapshot(t)
	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unsorted consumers",
			content: strings.Replace(
				string(content),
				`"consumer_targets": [
        "antigravity-cli",
        "codex"
      ]`,
				`"consumer_targets": [
        "codex",
        "antigravity-cli"
      ]`,
				1,
			),
			want: "sorted and duplicate-free",
		},
		{
			name: "duplicate semantic history key",
			content: strings.Replace(
				string(content),
				`"delegate_attempts": [`,
				`"delegate_attempts": [`+delegateAttemptJSONFromSnapshot(t, snapshot)+`,`,
				1,
			),
			want: "duplicate semantic key",
		},
		{
			name: "duplicate pending carrier identity",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierInstalls = append(
					persisted.PendingCarrierInstalls,
					persisted.PendingCarrierInstalls[0],
				)
			}),
			want: "duplicates one owner relation",
		},
		{
			name: "forged carrier subject",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierInstalls[0].Identity.CarrierSubject.Name = "forged"
			}),
			want: "carrier_subject does not match",
		},
		{
			name: "managed instance key drift",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierInstalls[0].Identity.ManagedInstanceKey = "managed/forged"
			}),
			want: "does not match carrier and subject identity",
		},
		{
			name: "unsupported claim provenance",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.ManagedCarrierClaims[0].Provenance = "attempt_succeeded"
			}),
			want: "unsupported managed carrier claim provenance",
		},
		{
			name: "unknown nested carrier field",
			content: strings.Replace(
				string(content),
				`"carrier_family": "claude-code-plugin",`,
				`"carrier_family": "claude-code-plugin",
        "remove_route": "host-defined",`,
				1,
			),
			want: "unknown field",
		},
		{
			name: "missing pending removal effect requirements",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierRemovals[0].EffectPostconditions = nil
			}),
			want: "effect_postconditions is required",
		},
		{
			name: "missing pending removal effect baselines",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierRemovals[0].EffectBaselines = nil
			}),
			want: "effect_baselines is required",
		},
		{
			name: "unsupported pending removal effect baseline",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.PendingCarrierRemovals[0].EffectBaselines = []effectBaselineDTO{{
					Requirement: string(effectpostcondition.CarrierArtifactsAbsent),
					State:       string(durablecarrier.EffectBaselineAbsent),
				}}
			}),
			want: "does not admit pre-effect content",
		},
		{
			name: "missing host attempt effect summaries",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.HostRouteAttempts[0].EffectPostconditions = nil
			}),
			want: "effect_postconditions is required",
		},
		{
			name: "unknown host attempt effect summary state",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.HostRouteAttempts[0].EffectPostconditions[0].State = "future"
			}),
			want: "summary state",
		},
		{
			name: "duplicate host attempt effect summaries",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.HostRouteAttempts[0].EffectPostconditions = append(
					persisted.HostRouteAttempts[0].EffectPostconditions,
					persisted.HostRouteAttempts[0].EffectPostconditions[0],
				)
			}),
			want: "duplicated",
		},
		{
			name: "effect reason summary mismatch",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.HostRouteAttempts[0].EffectPostconditions[0].State = "stale"
			}),
			want: "requires a matching effect postcondition summary",
		},
		{
			name:    "invented result class",
			content: strings.Replace(string(content), `"attempted_unverified"`, `"installed"`, 1),
			want:    "unsupported host route result class",
		},
		{
			name:    "missing operation",
			content: strings.Replace(string(content), `"operation": "remove",`, `"operation": "",`, 1),
			want:    "operation kind",
		},
		{
			name:    "invented operation",
			content: strings.Replace(string(content), `"operation": "remove"`, `"operation": "upgrade"`, 1),
			want:    "operation kind",
		},
		{
			name:    "retired history only result class",
			content: strings.Replace(string(content), `"attempted_unverified"`, `"history_only"`, 1),
			want:    "unsupported host route result class",
		},
		{
			name:    "retired unsupported result class",
			content: strings.Replace(string(content), `"attempted_unverified"`, `"unsupported"`, 1),
			want:    "unsupported host route result class",
		},
		{
			name:    "retired prior attempt only reason",
			content: strings.Replace(string(content), `"effect_postcondition_unavailable"`, `"prior_attempt_only"`, 1),
			want:    "unsupported host route result reason",
		},
		{
			name:    "invented observation summary",
			content: strings.Replace(string(content), `"observation": "present"`, `"observation": "ready"`, 1),
			want:    "relation observation summary",
		},
		{
			name:    "invented postcondition summary",
			content: strings.Replace(string(content), `"postcondition": "not_observed"`, `"postcondition": "installed"`, 1),
			want:    "relation postcondition summary",
		},
		{
			name: "transient delegate output field",
			content: strings.Replace(
				string(content),
				`"redacted": true`,
				`"redacted": true,
      "stdout": "/Users/alice/.cache/host-output"`,
				1,
			),
			want: "unknown field",
		},
		{
			name: "malformed route request hash",
			content: mutatedSnapshotJSON(t, snapshot, func(persisted *snapshotDTO) {
				persisted.HostRouteAttempts[0].RouteRequestHash = "sha256:short"
			}),
			want: "SHA-256 digest",
		},
		{
			name: "malformed managed path hash",
			content: strings.Replace(
				string(content),
				string(snapshot.ManagedPaths()[0].ContentHash()),
				"sha256:short",
				1,
			),
			want: "lowercase SHA-256 digest",
		},
		{
			name: "escaping managed path destination",
			content: strings.Replace(
				string(content),
				`"destination": ".agents/skills/oracle"`,
				`"destination": "../escape"`,
				1,
			),
			want: "stay inside its selected root",
		},
		{
			name: "scope contradictory managed path destination",
			content: strings.Replace(
				string(content),
				`"destination": ".agents/skills/oracle"`,
				`"destination": "~/agents/skills/oracle"`,
				1,
			),
			want: "project destination must be relative",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.content == string(content) {
				t.Fatalf("fixture replacement for %q did not change content", test.name)
			}
			_, err := Decode([]byte(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadOptionalDistinguishesMissingFromMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	snapshot, err := LoadOptional(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Equal(durable.EmptySnapshot()) {
		t.Fatal("missing optional state did not return empty snapshot")
	}
	if err := os.WriteFile(path, []byte(`{"version":8}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOptional(t.Context(), path); err == nil {
		t.Fatal("malformed existing state was treated as absent")
	}
}

func TestLoadValidatesCarrierAuthorityAgainstSelectedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".daem", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	statefileAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, request := testV8CarrierIdentity(t, "context7", "context7@official")
	owner, err := stateauthority.New(statefileAuthority.Exact(), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), path); err != nil {
		t.Fatalf("Load exact authority: %v", err)
	}

	foreignPath := filepath.Join(root, ".daem", "foreign.json")
	if err := os.WriteFile(foreignPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), foreignPath); err == nil ||
		!strings.Contains(err.Error(), "foreign statefile authority") {
		t.Fatalf("Load foreign authority error = %v", err)
	}
}

func TestValidateLoadedStateAuthorityRejectsForeignPersistedKey(t *testing.T) {
	statefilePath := filepath.Join(t.TempDir(), "State.json")
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	persistedKey := filepath.Join(string(filepath.Separator), "foreign", ".daem", "state.json")
	identity, request := testV8CarrierIdentity(t, "context7", "context7@official")
	owner, err := stateauthority.New(pathtest.Exact(
		persistedKey,
	),

		filepath.Join(string(filepath.Separator), "project", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateLoadedStateAuthority(snapshot, authority)
	if err == nil || !strings.Contains(err.Error(), "foreign statefile authority") ||
		!strings.Contains(err.Error(), "semantics") {
		t.Fatalf("authority validation error = %v", err)
	}
}

func TestLoadRequiresContextBeforePathInspection(t *testing.T) {
	_, err := Load(nil, filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "statefile context is required") {
		t.Fatalf("Load error = %v, want required-context rejection", err)
	}
}

func testV8Snapshot(t *testing.T) durable.Snapshot {
	t.Helper()
	pathSubject := testV8ProjectionSubject(t, entity.KindSkill, "oracle", "skill.project.agents")
	pathState, err := durable.NewManagedPathState(
		pathSubject,
		[]target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.HashFileContent([]byte("oracle")),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	aggregateState := testV8AggregateState(t)
	authorityRoot := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(authorityRoot, ".daem", "state.json"),
	),

		filepath.Join(authorityRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pendingIdentity, pendingRequest := testV8CarrierIdentity(
		t,
		"context7-pending",
		"context7-pending@official",
	)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, pendingIdentity, pendingRequest)
	if err != nil {
		t.Fatal(err)
	}
	claimIdentity, claimRequest := testV8CarrierIdentity(
		t,
		"context7-claimed",
		"context7-claimed@official",
	)
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		claimIdentity,
		claimRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	removeRequest, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.remove",
		"v1",
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}
	effectRequirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pendingRemoval, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removeRequest,
		effectRequirements,
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}
	hostSubject := pendingIdentity.RelationSubject()
	delegateSubject := testV8ProjectionSubject(
		t,
		entity.KindMCPServer,
		"context7",
		"mcp-server.project.claude-code",
	)
	exitCode := 7
	delegateAttempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         delegateSubject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "delegate:identity",
		ObservedAt:      time.Date(2026, 7, 18, 1, 2, 3, 4, time.UTC),
		Status:          durableattempt.DelegateStatusFailed,
		Reason:          durableattempt.DelegateReasonNonZeroExit,
		Observation:     observerelation.ObservationPresent,
		Postcondition:   observerelation.PostconditionNotObserved,
		ExitCode:        &exitCode,
		Redacted:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	effectSummary, err := assurancepostcondition.NewSummary(
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryUnavailable,
	)
	if err != nil {
		t.Fatal(err)
	}
	effectSummaries, err := assurancepostcondition.NewSummarySet(
		[]assurancepostcondition.Summary{effectSummary},
	)
	if err != nil {
		t.Fatal(err)
	}
	hostAttempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:              hostSubject,
		Target:               target.TargetClaudeCode,
		Scope:                target.ScopeProject,
		Operation:            lock.OperationRemove,
		RouteID:              removeRequest.RouteID(),
		RouteRequestHash:     removeRequest.CanonicalRequestHash(),
		ObservedAt:           time.Date(2026, 7, 18, 1, 2, 4, 5, time.UTC),
		ResultClass:          durableattempt.HostRouteResultAttemptedUnverified,
		Reason:               durableattempt.HostRouteReasonEffectUnavailable,
		AttemptObserved:      true,
		Observation:          observerelation.ObservationMissing,
		Postcondition:        observerelation.PostconditionObserved,
		EffectPostconditions: effectSummaries,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths:           []durable.ManagedPathState{pathState},
		ManagedAggregates:      []durable.ManagedAggregateState{aggregateState},
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pendingRemoval},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
		DelegateAttempts:       []durableattempt.DelegateAttempt{delegateAttempt},
		HostRouteAttempts:      []durableattempt.HostRouteAttempt{hostAttempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testV8CarrierIdentity(
	t *testing.T,
	name string,
	sourceRef string,
) (durablecarrier.ManagedCarrierIdentity, realizationdelegate.Request) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		sourceRef,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	relationSubject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(sourceRef)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		value.ID(),
		value.CarrierKey(),
		relationSubject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	return identity, request
}

func testV8AggregateState(t *testing.T) durable.ManagedAggregateState {
	t.Helper()
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: "echo guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	subject := testV8ProjectionSubject(t, entity.KindHook, "guard", string(placement.ID()))
	state, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testV8ProjectionSubject(
	t *testing.T,
	kind entity.Kind,
	name string,
	namespace string,
) topology.SubjectID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, namespace)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func delegateAttemptJSONFromSnapshot(t *testing.T, snapshot durable.Snapshot) string {
	t.Helper()
	persisted := persistedSnapshot(snapshot)
	if len(persisted.DelegateAttempts) != 1 {
		t.Fatalf("delegate attempt fixture count = %d", len(persisted.DelegateAttempts))
	}
	content, err := json.Marshal(persisted.DelegateAttempts[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func mutatedSnapshotJSON(
	t *testing.T,
	snapshot durable.Snapshot,
	mutate func(*snapshotDTO),
) string {
	t.Helper()
	persisted := persistedSnapshot(snapshot)
	mutate(&persisted)
	content, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
