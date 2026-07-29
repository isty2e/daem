package clijson

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

type Lock struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Mode          string `json:"mode"`
	ManifestPath  string `json:"manifest_path"`
	LockfilePath  string `json:"lockfile_path"`
	PreviousFound bool   `json:"previous_found"`
	EntryCounts   struct {
		Subjects         int `json:"subjects"`
		OrderConstraints int `json:"order_constraints"`
	} `json:"entry_counts"`
	ChangeCounts struct {
		Added     int `json:"added"`
		Changed   int `json:"changed"`
		Removed   int `json:"removed"`
		Unchanged int `json:"unchanged"`
	} `json:"change_counts"`
	OrderChangeCounts struct {
		Added     int `json:"added"`
		Changed   int `json:"changed"`
		Removed   int `json:"removed"`
		Unchanged int `json:"unchanged"`
	} `json:"order_change_counts"`
	HasChanges             bool                        `json:"has_changes"`
	SubjectChanges         []LockSubjectChange         `json:"subject_changes"`
	OrderConstraintChanges []LockOrderConstraintChange `json:"order_constraint_changes"`
}

type LockOrderConstraintChange struct {
	Status  string               `json:"status"`
	ClassID string               `json:"class_id"`
	Before  *LockOrderConstraint `json:"before"`
	After   *LockOrderConstraint `json:"after"`
}

type LockOrderConstraint struct {
	ClassID         string            `json:"class_id"`
	ContractVersion string            `json:"contract_version"`
	RuntimeMeaning  string            `json:"runtime_meaning"`
	Members         []LockOrderMember `json:"members"`
}

type LockOrderMember struct {
	Subject          LockSubjectID `json:"subject"`
	HostLoadIdentity string        `json:"host_load_identity"`
}

type LockSubjectChange struct {
	Status  string        `json:"status"`
	Subject LockSubjectID `json:"subject"`
	Before  *LockSubject  `json:"before"`
	After   *LockSubject  `json:"after"`
}

type LockSubjectID struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type LockSubject struct {
	EntityID       string                   `json:"entity_id"`
	Subject        LockSubjectID            `json:"subject"`
	Ownership      string                   `json:"ownership"`
	OnAbsent       string                   `json:"on_absent"`
	ExactSupply    *LockExactSupply         `json:"exact_supply"`
	ExactFileUse   *LockExactFileUse        `json:"exact_file_use"`
	Realization    *LockRealization         `json:"realization"`
	RepairHash     string                   `json:"repair_recipe_hash"`
	DelegatePlan   *LockDelegatePlan        `json:"delegate_plan"`
	SkillSetMember *LockSkillSetCorrelation `json:"skill_set_member"`
	Operations     []string                 `json:"operations"`
}

type LockExactSupply struct {
	SourceID    string `json:"source_id"`
	ResolvedRef string `json:"resolved_ref"`
	Kind        string `json:"kind"`
	ContentHash string `json:"content_hash"`
}

type LockExactFileUse struct {
	Scope      string `json:"scope"`
	Executable bool   `json:"executable"`
}

type LockRealization struct {
	Kind                   string   `json:"kind"`
	PlacementID            string   `json:"placement_id"`
	Target                 string   `json:"target"`
	ConsumerTargets        []string `json:"consumer_targets"`
	Scope                  string   `json:"scope"`
	Destination            string   `json:"destination"`
	ContentKind            string   `json:"content_kind"`
	PlacementMode          string   `json:"placement_mode"`
	PermissionPolicy       string   `json:"permission_policy"`
	ExactPermissionMode    *uint32  `json:"exact_permission_mode"`
	AggregateRoot          string   `json:"aggregate_root"`
	ContentPath            string   `json:"content_path"`
	MergeUnit              string   `json:"merge_unit"`
	SiblingRetention       string   `json:"sibling_retention"`
	Equivalence            string   `json:"equivalence"`
	SourceNamespace        string   `json:"source_namespace"`
	RelationSubjectKey     string   `json:"relation_subject_key"`
	ManagedInstanceKey     string   `json:"managed_instance_key"`
	RouteID                string   `json:"route_id"`
	CanonicalRequestHash   string   `json:"canonical_request_hash"`
	RouteContractVersion   string   `json:"route_contract_version"`
	AdapterContractVersion string   `json:"adapter_contract_version"`
	ComparedFields         []string `json:"compared_fields"`
	VerifiedRelationFields []string `json:"verified_relation_fields"`
}

type LockDelegatePlan struct {
	IdentityKey string `json:"identity_key"`
	RunnerKind  string `json:"runner_kind"`
}

type LockSkillSetCorrelation struct {
	DeclarationIdentity string `json:"declaration_identity"`
}

func DecodeLock(t testing.TB, content []byte) Lock {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var payload Lock
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("Decode returned error: %v\nstdout = %s", err, string(content))
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("Decode trailing returned %v, want EOF\nstdout = %s", err, string(content))
	}
	return payload
}
