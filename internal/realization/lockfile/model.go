package lockfile

type fileDTO struct {
	Version int              `toml:"version"`
	Locked  lockedSectionDTO `toml:"locked"`
}

type lockedSectionDTO struct {
	Subjects []lockedSubjectDTO `toml:"subject,omitempty"`
}

type lockedSubjectDTO struct {
	EntityID       string                        `toml:"entity_id"`
	SubjectID      string                        `toml:"subject_id"`
	ExactSupply    *exactIdentityDTO             `toml:"exact_supply,omitempty"`
	ExactFileUse   *exactFileUseDTO              `toml:"exact_file_use,omitempty"`
	Realization    *realizationDTO               `toml:"realization,omitempty"`
	Derivation     *derivationDTO                `toml:"derivation,omitempty"`
	RepairRecipe   *repairRecipeDTO              `toml:"repair_recipe,omitempty"`
	DelegatePlan   *delegatePlanDTO              `toml:"delegate_plan,omitempty"`
	SkillSetMember *skillSetMemberCorrelationDTO `toml:"skill_set_member,omitempty"`
	Ownership      string                        `toml:"ownership"`
	OnAbsent       string                        `toml:"on_absent"`
	Replay         replayCoverageDTO             `toml:"replay"`
	Operations     []operationContractDTO        `toml:"operation"`
}

type exactIdentityDTO struct {
	SourceID    string `toml:"source_id"`
	ResolvedRef string `toml:"resolved_ref,omitempty"`
	Kind        string `toml:"kind"`
	ContentHash string `toml:"content_hash"`
}

type exactFileUseDTO struct {
	Scope      string `toml:"scope"`
	Executable *bool  `toml:"executable"`
}

type skillSetMemberCorrelationDTO struct {
	DeclarationIdentity string `toml:"declaration_identity"`
}

type realizationDTO struct {
	ManagedPath       *managedPathProjectionDTO        `toml:"managed_path,omitempty"`
	ManagedAggregate  *managedAggregateContributionDTO `toml:"managed_aggregate,omitempty"`
	DelegatedRelation *delegatedRelationDTO            `toml:"delegated_relation,omitempty"`
}

type managedPathProjectionDTO struct {
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

type managedAggregateContributionDTO struct {
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

type delegatedRelationDTO struct {
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

type derivationDTO struct {
	DirectResolution       *exactIdentityDTO          `toml:"direct_resolution,omitempty"`
	DeterministicTransform *deterministicTransformDTO `toml:"deterministic_transform,omitempty"`
}

type deterministicTransformDTO struct {
	InputIdentity          exactIdentityDTO `toml:"input_identity"`
	RecipeHash             string           `toml:"recipe_hash"`
	AlgorithmID            string           `toml:"algorithm_id"`
	AlgorithmVersion       string           `toml:"algorithm_version"`
	ExecutionDomain        string           `toml:"execution_domain"`
	ExpectedOutputIdentity exactIdentityDTO `toml:"expected_output_identity"`
}

type repairRecipeDTO struct {
	Version    int                  `toml:"version"`
	RecipeHash string               `toml:"recipe_hash"`
	Input      exactIdentityDTO     `toml:"input"`
	Output     exactIdentityDTO     `toml:"output"`
	Operations []repairOperationDTO `toml:"operation"`
}

type repairOperationDTO struct {
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

type replayCoverageDTO struct {
	Invocation string               `toml:"invocation"`
	Outcome    string               `toml:"outcome"`
	Derivation string               `toml:"derivation"`
	Exclusions []replayExclusionDTO `toml:"exclusion,omitempty"`
}

type replayExclusionDTO struct {
	Component string `toml:"component"`
	Reason    string `toml:"reason"`
}

type delegatePlanDTO struct {
	IdentityKey string              `toml:"identity_key"`
	RunnerKind  string              `toml:"runner_kind"`
	Command     string              `toml:"command"`
	Args        []string            `toml:"args,omitempty"`
	Env         []delegateEnvDTO    `toml:"env,omitempty"`
	Package     *delegatePackageDTO `toml:"package,omitempty"`
	PinPolicy   string              `toml:"pin_policy"`
}

type delegateEnvDTO struct {
	Name       string `toml:"name"`
	SourceName string `toml:"source_name"`
}

type delegatePackageDTO struct {
	Ecosystem string `toml:"ecosystem"`
	Name      string `toml:"name"`
	Selector  string `toml:"selector,omitempty"`
}

type operationContractDTO struct {
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
