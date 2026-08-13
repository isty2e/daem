package lockfile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestClaudePluginCarrierSubjectRoundTripsLockfile(t *testing.T) {
	contract := lockfileClaudePluginCarrierContract(t, desiredextension.SourceKindMarketplace, "team/context7:beta@official", "context7", "context7")
	file := lockfileWithSubjects(t, contract)
	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	for _, want := range []string{
		`entity_id = "extension:context7"`,
		`subject_id = "host_relation/claude-code.plugin-carrier/context7"`,
		"[locked.subject.realization.delegated_relation]",
		`source_namespace = "marketplace:team/context7:beta@official"`,
		`relation_subject_key = "context7"`,
		`managed_instance_key = "host-relation:v1:`,
		`route_id = "claude-code.plugin-carrier.install"`,
		`route_contract_version = "claude-plugin-carrier-v1"`,
		`canonical_request_hash = "sha256:`,
		`verified_relation_fields = ["managed_instance_key", "relation_subject_key", "scope", "source_kind", "source_ref", "target"]`,
		`invocation = "partial"`,
		`outcome = "unavailable"`,
		`derivation = "not_applicable"`,
		`operation = "install"`,
		`actuation = "delegated_host_route"`,
		`effect_envelope = "incomplete"`,
		`idempotency = "unknown"`,
		`trust_activation = "unknown"`,
		`recovery = "unknown"`,
		`operation = "refresh"`,
		`route_id = "claude-code.plugin-carrier.refresh"`,
		`adapter_contract_version = "claude-plugin-refresh-v1"`,
		`authority = "none"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("lockfile = %s, want %q", rendered, want)
		}
	}
	for _, forbidden := range []string{
		`artifact_kind`, `content_hash`, `delegate_plan`, `observed_at`, `cache_path`,
		`runtime_readiness`, `tool_inventory`, `route_request_hash`, `[locked.subject.claim]`,
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("lockfile = %s, must not contain %q", rendered, forbidden)
		}
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
	carrier, ok, err := lock.DelegatedRelationCarrier(loaded.Locked.Subjects()[0])
	if err != nil {
		t.Fatalf("DelegatedRelationCarrier returned error: %v", err)
	}
	if !ok {
		t.Fatal("DelegatedRelationCarrier returned ok=false")
	}
	if carrier != desiredextension.CarrierClaudeCodePlugin {
		t.Fatalf("carrier = %q", carrier)
	}
	realization, _ := loaded.Locked.Subjects()[0].Realization()
	relation, _ := realization.DelegatedRelation()
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil || source.Ref() != "team/context7:beta@official" {
		t.Fatalf("source = %#v, %v", source, err)
	}
}

func TestClaudePluginCarrierSubjectRejectsLockfileDrift(t *testing.T) {
	contract := lockfileClaudePluginCarrierContract(t, desiredextension.SourceKindMarketplace, "context7@official", "context7-managed", "context7@official")
	content, err := Marshal(lockfileWithSubjects(t, contract))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	tests := []struct {
		name        string
		old         string
		replacement string
	}{
		{name: "source namespace", old: `source_namespace = "marketplace:context7@official"`, replacement: `source_namespace = "marketplace:other@official"`},
		{name: "target", old: `target = "claude-code"`, replacement: `target = "codex"`},
		{name: "project scope", old: `scope = "project"`, replacement: `scope = "global"`},
		{name: "entity id", old: `entity_id = "extension:context7-managed"`, replacement: `entity_id = "extension:renamed-context7"`},
		{name: "marketplace-derived plugin key", old: `relation_subject_key = "context7@official"`, replacement: `relation_subject_key = "other-context7@official"`},
		{name: "canonical request hash", old: `canonical_request_hash = "sha256:`, replacement: `canonical_request_hash = "sha256:0000`},
		{name: "refresh operation", old: `operation = "refresh"`, replacement: `operation = "install"`},
		{name: "refresh route id", old: `route_id = "claude-code.plugin-carrier.refresh"`, replacement: `route_id = "claude-code.plugin-carrier.install"`},
		{name: "refresh adapter contract", old: `adapter_contract_version = "claude-plugin-refresh-v1"`, replacement: `adapter_contract_version = "claude-plugin-refresh-v2"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeLockfileText(t, string(replaceLockfileOnce(t, content, test.old, test.replacement)))
			if _, loadErr := Load(t.Context(), path); loadErr == nil {
				t.Fatal("Load returned nil error, want canonical delegated-relation drift rejection")
			}
		})
	}
}

func lockfileClaudePluginCarrierContract(
	t *testing.T,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
	declarationID string,
	subjectKey string,
) lock.LockedSubjectContract {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceRef)
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey returned error: %v", err)
	}
	visibleKey, err := hostrelation.NewSubjectKey(subjectKey)
	if err != nil {
		t.Fatalf("NewSubjectKey returned error: %v", err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		desiredEntityID(t, entity.KindExtension, declarationID),
		carrier,
		lockfileClaudeCarrierSubjectID(t, declarationID),
		visibleKey,
	)
	if err != nil {
		t.Fatalf("NewDelegatedRelationCarrierContract returned error: %v", err)
	}
	return contract
}

func lockfileClaudeCarrierSubjectID(t *testing.T, declarationID string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", declarationID)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}

func replaceLockfileOnce(t *testing.T, content []byte, old string, replacement string) []byte {
	t.Helper()
	text := string(content)
	if !strings.Contains(text, old) {
		t.Fatalf("lockfile content missing %q:\n%s", old, text)
	}
	return []byte(strings.Replace(text, old, replacement, 1))
}
