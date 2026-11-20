package lockfile

// Schema 6 as written by the released daem versions. Keep the entire wire
// tree independent so new fields on current records cannot be read as v6.
type v6FileDTO struct {
	Version int64              `toml:"version"`
	Locked  v6LockedSectionDTO `toml:"locked"`
}

type v6LockedSectionDTO struct {
	Subjects         []v6LockedSubjectDTO         `toml:"subject,omitempty"`
	OrderConstraints []v6LockedOrderConstraintDTO `toml:"order_constraint,omitempty"`
}

type v6LockedOrderConstraintDTO struct {
	ClassID         string                   `toml:"class_id"`
	ContractVersion string                   `toml:"contract_version"`
	RuntimeMeaning  string                   `toml:"runtime_meaning"`
	Members         []v6LockedOrderMemberDTO `toml:"member"`
}

type v6LockedOrderMemberDTO struct {
	SubjectID        string `toml:"subject_id"`
	HostLoadIdentity string `toml:"host_load_identity"`
}

type v6LockedSubjectDTO struct {
	EntityID                string                          `toml:"entity_id"`
	SubjectID               string                          `toml:"subject_id"`
	ExactSupply             *v6ExactIdentityDTO             `toml:"exact_supply,omitempty"`
	ExactFileUse            *v6ExactFileUseDTO              `toml:"exact_file_use,omitempty"`
	Realization             *v6RealizationDTO               `toml:"realization,omitempty"`
	Derivation              *v6DerivationDTO                `toml:"derivation,omitempty"`
	RepairRecipe            *v6RepairRecipeDTO              `toml:"repair_recipe,omitempty"`
	DelegatePlan            *v6DelegatePlanDTO              `toml:"delegate_plan,omitempty"`
	MCPEnvironmentSources   []string                        `toml:"mcp_environment_sources,omitempty"`
	MCPProviderContribution string                          `toml:"mcp_provider_contribution,omitempty"`
	SkillSetMember          *v6SkillSetMemberCorrelationDTO `toml:"skill_set_member,omitempty"`
	Ownership               string                          `toml:"ownership"`
	OnAbsent                string                          `toml:"on_absent"`
	Replay                  v6ReplayCoverageDTO             `toml:"replay"`
	Operations              []v6OperationContractDTO        `toml:"operation"`
}

type v6ExactIdentityDTO struct {
	SourceID    string `toml:"source_id"`
	ResolvedRef string `toml:"resolved_ref,omitempty"`
	Kind        string `toml:"kind"`
	ContentHash string `toml:"content_hash"`
}

type v6ExactFileUseDTO struct {
	Scope      string `toml:"scope"`
	Executable *bool  `toml:"executable"`
}

type v6SkillSetMemberCorrelationDTO struct {
	DeclarationIdentity string `toml:"declaration_identity"`
}

type v6RealizationDTO struct {
	ManagedPath       *v6ManagedPathProjectionDTO        `toml:"managed_path,omitempty"`
	ManagedAggregate  *v6ManagedAggregateContributionDTO `toml:"managed_aggregate,omitempty"`
	DelegatedRelation *v6DelegatedRelationDTO            `toml:"delegated_relation,omitempty"`
}

type v6ManagedPathProjectionDTO struct {
	PlacementID            string   `toml:"placement_id"`
	ConsumerTargets        []string `toml:"consumer_targets"`
	Scope                  string   `toml:"scope"`
	Destination            string   `toml:"destination"`
	ContentKind            string   `toml:"content_kind"`
	PlacementMode          string   `toml:"placement_mode"`
	PermissionPolicy       string   `toml:"permission_policy"`
	ExactPermissionMode    *uint32  `toml:"exact_permission_mode,omitempty"`
	AdapterContractVersion string   `toml:"adapter_contract_version"`
}

type v6ManagedAggregateContributionDTO struct {
	PlacementID             string   `toml:"placement_id"`
	Target                  string   `toml:"target"`
	Scope                   string   `toml:"scope"`
	AggregateRoot           string   `toml:"aggregate_root"`
	ContentPath             string   `toml:"content_path"`
	MergeUnit               string   `toml:"merge_unit"`
	ContributionCardinality string   `toml:"contribution_cardinality"`
	SiblingRetention        string   `toml:"sibling_retention"`
	SiblingPreservation     string   `toml:"sibling_preservation"`
	Equivalence             string   `toml:"equivalence"`
	CanonicalContribution   string   `toml:"canonical_contribution"`
	CodecContract           string   `toml:"codec_contract"`
	ComparedFields          []string `toml:"compared_fields"`
}

type v6DelegatedRelationDTO struct {
	PlacementID            string   `toml:"placement_id"`
	Target                 string   `toml:"target"`
	Scope                  string   `toml:"scope"`
	SourceNamespace        string   `toml:"source_namespace"`
	RelationSubjectKey     string   `toml:"relation_subject_key"`
	ManagedInstanceKey     string   `toml:"managed_instance_key"`
	RouteID                string   `toml:"route_id"`
	RouteContractVersion   string   `toml:"route_contract_version"`
	CanonicalRequestHash   string   `toml:"canonical_request_hash"`
	VerifiedRelationFields []string `toml:"verified_relation_fields"`
}

type v6DerivationDTO struct {
	DirectResolution       *v6ExactIdentityDTO          `toml:"direct_resolution,omitempty"`
	DeterministicTransform *v6DeterministicTransformDTO `toml:"deterministic_transform,omitempty"`
}

type v6DeterministicTransformDTO struct {
	InputIdentity          v6ExactIdentityDTO `toml:"input_identity"`
	RecipeHash             string             `toml:"recipe_hash"`
	AlgorithmID            string             `toml:"algorithm_id"`
	AlgorithmVersion       string             `toml:"algorithm_version"`
	ExecutionDomain        string             `toml:"execution_domain"`
	ExpectedOutputIdentity v6ExactIdentityDTO `toml:"expected_output_identity"`
}

type v6RepairRecipeDTO struct {
	Version    int64                  `toml:"version"`
	RecipeHash string                 `toml:"recipe_hash"`
	Input      v6ExactIdentityDTO     `toml:"input"`
	Output     v6ExactIdentityDTO     `toml:"output"`
	Operations []v6RepairOperationDTO `toml:"operation"`
}

type v6RepairOperationDTO struct {
	Kind            string `toml:"kind"`
	From            string `toml:"from,omitempty"`
	To              string `toml:"to,omitempty"`
	Path            string `toml:"path,omitempty"`
	Field           string `toml:"field,omitempty"`
	OldValue        string `toml:"old_value,omitempty"`
	OldValuePresent *bool  `toml:"old_value_present,omitempty"`
	NewValue        string `toml:"new_value,omitempty"`
	Offset          *int   `toml:"offset,omitempty"`
	OldBytesBase64  string `toml:"old_bytes_base64,omitempty"`
	NewBytesBase64  string `toml:"new_bytes_base64,omitempty"`
	FileHash        string `toml:"file_hash,omitempty"`
	InputHash       string `toml:"input_hash,omitempty"`
	OutputHash      string `toml:"output_hash,omitempty"`
	Mode            *int   `toml:"mode,omitempty"`
}

type v6ReplayCoverageDTO struct {
	Invocation string                 `toml:"invocation"`
	Outcome    string                 `toml:"outcome"`
	Derivation string                 `toml:"derivation"`
	Exclusions []v6ReplayExclusionDTO `toml:"exclusion,omitempty"`
}

type v6ReplayExclusionDTO struct {
	Component string `toml:"component"`
	Reason    string `toml:"reason"`
}

type v6DelegatePlanDTO struct {
	IdentityKey string                 `toml:"identity_key"`
	RunnerKind  string                 `toml:"runner_kind"`
	Command     string                 `toml:"command"`
	Args        []string               `toml:"args,omitempty"`
	Env         []v6DelegateEnvDTO     `toml:"env,omitempty"`
	Packages    []v6DelegatePackageDTO `toml:"package,omitempty"`
	PinPolicy   string                 `toml:"pin_policy"`
}

type v6DelegateEnvDTO struct {
	Name       string `toml:"name"`
	SourceName string `toml:"source_name"`
}

type v6DelegatePackageDTO struct {
	Ecosystem string `toml:"ecosystem"`
	Name      string `toml:"name"`
	Selector  string `toml:"selector,omitempty"`
}

type v6OperationContractDTO struct {
	Operation                   string   `toml:"operation"`
	Actuation                   string   `toml:"actuation"`
	Authority                   string   `toml:"authority"`
	RouteID                     string   `toml:"route_id,omitempty"`
	RouteAdapterContractVersion string   `toml:"route_adapter_contract_version,omitempty"`
	HostVersionConstraint       string   `toml:"host_version_constraint,omitempty"`
	ConfigFormatVersion         string   `toml:"config_format_version,omitempty"`
	Preconditions               []string `toml:"preconditions,omitempty"`
	EffectEnvelope              string   `toml:"effect_envelope"`
	EffectPostconditions        []string `toml:"effect_postconditions,omitempty"`
	Idempotency                 string   `toml:"idempotency"`
	Verification                string   `toml:"verification"`
	TrustActivation             string   `toml:"trust_activation"`
	Recovery                    string   `toml:"recovery"`
}
