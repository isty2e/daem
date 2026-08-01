package carrierclaim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type registryDTO struct {
	Version int        `json:"version"`
	Claims  []claimDTO `json:"claims"`
}

type claimDTO struct {
	Owner          authorityDTO `json:"owner"`
	Identity       identityDTO  `json:"identity"`
	InstallRequest requestDTO   `json:"install_request"`
	Provenance     string       `json:"provenance"`
}

type authorityDTO struct {
	StatefileAuthority pathAuthorityDTO `json:"statefile_authority"`
	ManifestPath       string           `json:"manifest_path"`
}

type pathAuthorityDTO struct {
	Key     string `json:"key"`
	Witness string `json:"semantics_witness"`
}

type identityDTO struct {
	CarrierSubject     subjectDTO `json:"carrier_subject"`
	CarrierFamily      string     `json:"carrier_family"`
	Target             string     `json:"target"`
	Scope              string     `json:"scope"`
	SourceKind         string     `json:"source_kind"`
	SourceRef          string     `json:"source_ref"`
	RelationSubject    subjectDTO `json:"relation_subject"`
	RelationSubjectKey string     `json:"relation_subject_key"`
	ManagedInstanceKey string     `json:"managed_instance_key"`
}

type requestDTO struct {
	RouteID                string `json:"route_id"`
	AdapterContractVersion string `json:"adapter_contract_version"`
	CanonicalRequestHash   string `json:"canonical_request_hash"`
}

type subjectDTO struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func encode(registry durablecarrier.GlobalCarrierClaims) ([]byte, error) {
	persisted := registryDTO{
		Version: currentVersion,
		Claims:  make([]claimDTO, 0, len(registry.Claims())),
	}
	for _, claim := range registry.Claims() {
		persisted.Claims = append(persisted.Claims, persistedClaim(claim))
	}
	content, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode carrier claim registry: %w", err)
	}
	content = append(content, '\n')
	if len(content) > maximumRegistryBytes {
		return nil, fmt.Errorf("carrier claim registry exceeds %d bytes", maximumRegistryBytes)
	}
	return content, nil
}

// Marshal renders one canonical global carrier-claim registry deterministically.
func Marshal(registry durablecarrier.GlobalCarrierClaims) ([]byte, error) {
	return encode(registry)
}

func persistedClaim(claim durablecarrier.ManagedCarrierClaim) claimDTO {
	identity := claim.Identity()
	key := identity.Carrier().Key()
	relation := identity.ExpectedRelation()
	request := claim.InstallRequest()
	statefile := claim.Owner().StatefileAuthority()
	return claimDTO{
		Owner: authorityDTO{
			StatefileAuthority: pathAuthorityDTO{
				Key:     statefile.Key(),
				Witness: statefile.Witness(),
			},
			ManifestPath: claim.Owner().ManifestPath(),
		},
		Identity: identityDTO{
			CarrierSubject:     persistedSubject(identity.CarrierSubject()),
			CarrierFamily:      string(identity.Carrier().Family()),
			Target:             string(identity.Target()),
			Scope:              string(identity.Scope()),
			SourceKind:         string(key.Source().Kind()),
			SourceRef:          key.Source().Ref(),
			RelationSubject:    persistedSubject(identity.RelationSubject()),
			RelationSubjectKey: string(relation.SubjectKey()),
			ManagedInstanceKey: string(relation.ManagedInstanceKey()),
		},
		InstallRequest: requestDTO{
			RouteID:                request.RouteID(),
			AdapterContractVersion: request.ContractVersion(),
			CanonicalRequestHash:   request.CanonicalRequestHash(),
		},
		Provenance: string(claim.Provenance()),
	}
}

func persistedSubject(subject topology.SubjectID) subjectDTO {
	return subjectDTO{
		Kind:      string(subject.Kind()),
		Namespace: subject.Namespace(),
		Name:      subject.Key(),
	}
}

func decode(content []byte) (durablecarrier.GlobalCarrierClaims, error) {
	if len(content) > maximumRegistryBytes {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"carrier claim registry exceeds %d bytes",
			maximumRegistryBytes,
		)
	}
	version, err := jsonstrict.ValidateVersionedObject(
		content,
		"carrier claim registry",
		maximumRegistryJSONDepth,
	)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	switch version {
	case currentVersion:
		return decodeCurrent(content)
	case retiredCarrierClaimRegistryVersion:
		return decodeRetiredCarrierClaimRegistry(content)
	default:
		return durablecarrier.GlobalCarrierClaims{}, unsupportedCarrierClaimRegistryVersion(version)
	}
}

func decodeCurrent(content []byte) (durablecarrier.GlobalCarrierClaims, error) {
	var persisted registryDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("decode carrier claim registry: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry contains multiple JSON values")
	} else if err != io.EOF {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("decode carrier claim registry trailer: %w", err)
	}
	if persisted.Version != currentVersion {
		return durablecarrier.GlobalCarrierClaims{}, unsupportedCarrierClaimRegistryVersion(persisted.Version)
	}
	if persisted.Claims == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry requires claims array")
	}
	claims := make([]durablecarrier.ManagedCarrierClaim, 0, len(persisted.Claims))
	for index, row := range persisted.Claims {
		claim, err := row.canonical()
		if err != nil {
			return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("claims[%d]: %w", index, err)
		}
		claims = append(claims, claim)
	}
	return durablecarrier.NewGlobalCarrierClaims(claims)
}

func unsupportedCarrierClaimRegistryVersion(version int) error {
	if version > currentVersion {
		return fmt.Errorf(
			"unsupported carrier claim registry version %d; it was written by a newer daem, so upgrade daem before reading it",
			version,
		)
	}
	return fmt.Errorf(
		"unsupported carrier claim registry version %d; use the daem version that wrote it to recover or retire managed carriers before upgrading",
		version,
	)
}

func (persisted claimDTO) canonical() (durablecarrier.ManagedCarrierClaim, error) {
	statefile, err := pathauthority.NewExact(
		persisted.Owner.StatefileAuthority.Key,
		persisted.Owner.StatefileAuthority.Witness,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("owner statefile authority: %w", err)
	}
	owner, err := stateauthority.New(statefile, persisted.Owner.ManifestPath)
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("owner: %w", err)
	}
	identity, err := persisted.Identity.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("identity: %w", err)
	}
	request, err := realizationdelegate.NewRequest(
		persisted.InstallRequest.RouteID,
		persisted.InstallRequest.AdapterContractVersion,
		persisted.InstallRequest.CanonicalRequestHash,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("install_request: %w", err)
	}
	return durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenance(persisted.Provenance),
	)
}

func (persisted identityDTO) canonical() (durablecarrier.ManagedCarrierIdentity, error) {
	carrierSubject, err := persisted.CarrierSubject.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("carrier_subject: %w", err)
	}
	family, err := desiredextension.ParseCarrier(persisted.CarrierFamily)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	selectedTarget, err := target.ParseTarget(persisted.Target)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKind(persisted.SourceKind),
		persisted.SourceRef,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	key, err := desiredextension.NewCarrierKey(family, selectedTarget, scope, source)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	if carrier.SubjectID() != carrierSubject {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf(
			"carrier_subject does not match canonical carrier identity",
		)
	}
	relationSubject, err := persisted.RelationSubject.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("relation_subject: %w", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(persisted.RelationSubjectKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	managedKey, err := hostrelation.NewManagedInstanceKey(persisted.ManagedInstanceKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	return durablecarrier.NewManagedCarrierIdentity(carrier, relationSubject, expected)
}

func (persisted subjectDTO) canonical() (topology.SubjectID, error) {
	return topology.NewSubjectID(
		topology.SubjectKind(persisted.Kind),
		persisted.Namespace,
		persisted.Name,
	)
}
