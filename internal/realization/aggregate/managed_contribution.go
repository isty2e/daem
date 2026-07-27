package aggregate

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// DocumentFileMode is the closed permission contract for directly managed
// aggregate documents.
const DocumentFileMode os.FileMode = 0o600

// MergeUnit identifies the smallest projection subtree rendered as one unit.
type MergeUnit string

// SiblingRetention identifies whether data outside a projection is retained.
type SiblingRetention string

// ContributionCardinality identifies whether one projection address admits one or many subjects.
type ContributionCardinality string

const (
	// PreserveUnmanagedSiblings is the only admitted partial-ownership policy.
	PreserveUnmanagedSiblings SiblingRetention = "preserve_unmanaged_siblings"
)

const (
	ContributionExclusive ContributionCardinality = "exclusive"
	ContributionSharedSet ContributionCardinality = "shared_set"
)

// SiblingPreservation identifies the fidelity promised for retained siblings.
type SiblingPreservation string

const (
	PreserveSiblingsByteExact SiblingPreservation = "byte_exact"
	PreserveSiblingsSemantic  SiblingPreservation = "canonical_semantic"
)

// Equivalence identifies how the owned projection is compared.
type Equivalence string

const (
	EquivalenceByteExact         Equivalence = "byte_exact"
	EquivalenceCanonicalSemantic Equivalence = "canonical_semantic"
)

// CodecContractID identifies one exact private aggregate codec contract.
type CodecContractID string

// ContentPath identifies one canonical managed projection inside a document.
type ContentPath string

// DocumentAddress identifies one portable host aggregate document.
type DocumentAddress struct {
	target target.Target
	scope  target.Scope
	root   output.Destination
}

// ProjectionAddress identifies one placement-owned projection in a document.
type ProjectionAddress struct {
	document    DocumentAddress
	placementID string
	mergeUnit   MergeUnit
	contentPath ContentPath
}

// ManagedContributionInput carries one subject's locked aggregate realization.
type ManagedContributionInput struct {
	PlacementID           string
	Target                target.Target
	Scope                 target.Scope
	AggregateRoot         output.Destination
	ContentPath           string
	MergeUnit             MergeUnit
	Cardinality           ContributionCardinality
	SiblingRetention      SiblingRetention
	SiblingPreservation   SiblingPreservation
	Equivalence           Equivalence
	CanonicalContribution string
	CodecContractID       CodecContractID
	ComparedFields        []string
}

// ManagedContribution is one subject's exact contribution to a shared projection.
type ManagedContribution struct {
	address               ProjectionAddress
	cardinality           ContributionCardinality
	siblingRetention      SiblingRetention
	siblingPreservation   SiblingPreservation
	equivalence           Equivalence
	canonicalContribution string
	codecContractID       CodecContractID
	comparedFields        []string
}

// ProjectionContract is the static codec and preservation contract for one address.
type ProjectionContract struct {
	address             ProjectionAddress
	cardinality         ContributionCardinality
	siblingRetention    SiblingRetention
	siblingPreservation SiblingPreservation
	equivalence         Equivalence
	codecContractID     CodecContractID
	comparedFields      []string
}

// NewManagedContribution constructs a validated aggregate realization body.
func NewManagedContribution(input ManagedContributionInput) (ManagedContribution, error) {
	address, err := newProjectionAddress(
		input.PlacementID,
		input.Target,
		input.Scope,
		input.AggregateRoot,
		input.MergeUnit,
		input.ContentPath,
	)
	if err != nil {
		return ManagedContribution{}, fmt.Errorf("managed aggregate contribution: %w", err)
	}
	contribution := ManagedContribution{
		address:               address,
		cardinality:           input.Cardinality,
		siblingRetention:      input.SiblingRetention,
		siblingPreservation:   input.SiblingPreservation,
		equivalence:           input.Equivalence,
		canonicalContribution: input.CanonicalContribution,
		codecContractID:       input.CodecContractID,
		comparedFields:        canonicalTokenSet(input.ComparedFields),
	}
	if err := contribution.Validate(); err != nil {
		return ManagedContribution{}, err
	}
	return contribution, nil
}

func newProjectionAddress(
	placementID string,
	selectedTarget target.Target,
	scope target.Scope,
	aggregateRoot output.Destination,
	mergeUnit MergeUnit,
	contentPath string,
) (ProjectionAddress, error) {
	if err := validateToken("aggregate placement id", placementID); err != nil {
		return ProjectionAddress{}, err
	}
	document, err := newDocumentAddress(selectedTarget, scope, aggregateRoot)
	if err != nil {
		return ProjectionAddress{}, err
	}
	parsedPath, err := ParseContentPath(contentPath)
	if err != nil {
		return ProjectionAddress{}, err
	}
	canonicalMergeUnit := mergeUnit
	if err := validateToken("aggregate merge unit", string(canonicalMergeUnit)); err != nil {
		return ProjectionAddress{}, err
	}
	return ProjectionAddress{
		document:    document,
		placementID: placementID,
		mergeUnit:   canonicalMergeUnit,
		contentPath: parsedPath,
	}, nil
}

// ParseContentPath parses the canonical slash-delimited projection address.
func ParseContentPath(value string) (ContentPath, error) {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("aggregate content path must be non-empty and trimmed")
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("aggregate content path must be valid UTF-8")
	}
	if strings.Contains(value, `\`) || !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "", fmt.Errorf("aggregate content path must be an absolute slash-delimited path without trailing slash")
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return "", fmt.Errorf("aggregate content path must not contain control characters")
	}
	if path.Clean(value) != value {
		return "", fmt.Errorf("aggregate content path must be canonical")
	}
	for segment := range strings.SplitSeq(strings.TrimPrefix(value, "/"), "/") {
		if err := validateToken("aggregate content path segment", segment); err != nil {
			return "", err
		}
	}
	return ContentPath(value), nil
}

// Validate rejects zero or forged contribution values.
func (contribution ManagedContribution) Validate() error {
	if err := validateProjectionContract(contribution.Contract()); err != nil {
		return err
	}
	if contribution.canonicalContribution == "" {
		return fmt.Errorf("managed aggregate contribution canonical contribution is required")
	}
	if !utf8.ValidString(contribution.canonicalContribution) {
		return fmt.Errorf("managed aggregate contribution canonical contribution must be valid UTF-8")
	}
	return nil
}

// Validate rejects zero or forged projection addresses.
func (address ProjectionAddress) Validate() error {
	canonical, err := newProjectionAddress(
		address.placementID,
		address.document.target,
		address.document.scope,
		address.document.root,
		address.mergeUnit,
		string(address.contentPath),
	)
	if err != nil {
		return err
	}
	if canonical != address {
		return fmt.Errorf("aggregate projection address is not canonical")
	}
	return nil
}

// Address returns the shared projection address for this contribution.
func (contribution ManagedContribution) Address() ProjectionAddress { return contribution.address }

// Contract returns the static projection contract without desired value bytes.
func (contribution ManagedContribution) Contract() ProjectionContract {
	return ProjectionContract{
		address:             contribution.address,
		cardinality:         contribution.cardinality,
		siblingRetention:    contribution.siblingRetention,
		siblingPreservation: contribution.siblingPreservation,
		equivalence:         contribution.equivalence,
		codecContractID:     contribution.codecContractID,
		comparedFields:      append([]string(nil), contribution.comparedFields...),
	}
}

func (contribution ManagedContribution) PlacementID() string { return contribution.address.placementID }

func (contribution ManagedContribution) Target() target.Target {
	return contribution.address.document.target
}

func (contribution ManagedContribution) Scope() target.Scope {
	return contribution.address.document.scope
}

func (contribution ManagedContribution) AggregateRoot() output.Destination {
	return contribution.address.document.root
}

func (contribution ManagedContribution) ContentPath() string {
	return string(contribution.address.contentPath)
}

func (contribution ManagedContribution) MergeUnit() MergeUnit { return contribution.address.mergeUnit }

func (contribution ManagedContribution) Cardinality() ContributionCardinality {
	return contribution.cardinality
}

func (contribution ManagedContribution) SiblingRetention() SiblingRetention {
	return contribution.siblingRetention
}

func (contribution ManagedContribution) SiblingPreservation() SiblingPreservation {
	return contribution.siblingPreservation
}

func (contribution ManagedContribution) Equivalence() Equivalence { return contribution.equivalence }

func (contribution ManagedContribution) CanonicalContribution() string {
	return contribution.canonicalContribution
}

func (contribution ManagedContribution) CodecContractID() CodecContractID {
	return contribution.codecContractID
}

func (contribution ManagedContribution) ComparedFields() []string {
	return append([]string(nil), contribution.comparedFields...)
}

// Clone returns a defensive contribution copy.
func (contribution ManagedContribution) Clone() ManagedContribution {
	contribution.comparedFields = append([]string(nil), contribution.comparedFields...)
	return contribution
}

// Equal compares every canonical contribution fact.
func (contribution ManagedContribution) Equal(other ManagedContribution) bool {
	return contribution.Validate() == nil && other.Validate() == nil &&
		contribution.address == other.address &&
		contribution.cardinality == other.cardinality &&
		contribution.siblingRetention == other.siblingRetention &&
		contribution.siblingPreservation == other.siblingPreservation &&
		contribution.equivalence == other.equivalence &&
		contribution.canonicalContribution == other.canonicalContribution &&
		contribution.codecContractID == other.codecContractID &&
		slices.Equal(contribution.comparedFields, other.comparedFields)
}

func (address ProjectionAddress) Document() DocumentAddress { return address.document }
func (address ProjectionAddress) PlacementID() string       { return address.placementID }
func (address ProjectionAddress) MergeUnit() MergeUnit      { return address.mergeUnit }
func (address ProjectionAddress) ContentPath() ContentPath  { return address.contentPath }

func (address DocumentAddress) Target() target.Target             { return address.target }
func (address DocumentAddress) Scope() target.Scope               { return address.scope }
func (address DocumentAddress) AggregateRoot() output.Destination { return address.root }

// Validate rejects zero or forged projection contracts.
func (contract ProjectionContract) Validate() error {
	return validateProjectionContract(contract)
}

func validateProjectionContract(contract ProjectionContract) error {
	if err := contract.address.Validate(); err != nil {
		return fmt.Errorf("managed aggregate contribution: %w", err)
	}
	switch contract.cardinality {
	case ContributionExclusive, ContributionSharedSet:
	default:
		return fmt.Errorf("managed aggregate contribution cardinality %q is unsupported", contract.cardinality)
	}
	if contract.siblingRetention != PreserveUnmanagedSiblings {
		return fmt.Errorf("managed aggregate contribution sibling retention %q is unsupported", contract.siblingRetention)
	}
	switch contract.siblingPreservation {
	case PreserveSiblingsByteExact, PreserveSiblingsSemantic:
	default:
		return fmt.Errorf("managed aggregate contribution sibling preservation %q is unsupported", contract.siblingPreservation)
	}
	if err := validateToken("aggregate codec contract id", string(contract.codecContractID)); err != nil {
		return fmt.Errorf("managed aggregate contribution: %w", err)
	}
	switch contract.equivalence {
	case EquivalenceByteExact:
		if len(contract.comparedFields) != 0 {
			return fmt.Errorf("byte-exact aggregate contribution must not include compared fields")
		}
	case EquivalenceCanonicalSemantic:
		if err := validateTokenSet("aggregate compared field", contract.comparedFields); err != nil {
			return err
		}
	default:
		return fmt.Errorf("aggregate equivalence %q is unsupported", contract.equivalence)
	}
	return nil
}

// Equal compares every static projection contract fact.
func (contract ProjectionContract) Equal(other ProjectionContract) bool {
	return contract.Validate() == nil && other.Validate() == nil &&
		contract.address == other.address &&
		contract.cardinality == other.cardinality &&
		contract.siblingRetention == other.siblingRetention &&
		contract.siblingPreservation == other.siblingPreservation &&
		contract.equivalence == other.equivalence &&
		contract.codecContractID == other.codecContractID &&
		slices.Equal(contract.comparedFields, other.comparedFields)
}

func (contract ProjectionContract) Address() ProjectionAddress { return contract.address }
func (contract ProjectionContract) Cardinality() ContributionCardinality {
	return contract.cardinality
}

func (contract ProjectionContract) SiblingRetention() SiblingRetention {
	return contract.siblingRetention
}

func (contract ProjectionContract) SiblingPreservation() SiblingPreservation {
	return contract.siblingPreservation
}
func (contract ProjectionContract) Equivalence() Equivalence { return contract.equivalence }
func (contract ProjectionContract) CodecContractID() CodecContractID {
	return contract.codecContractID
}

func (contract ProjectionContract) ComparedFields() []string {
	return append([]string(nil), contract.comparedFields...)
}

// Clone returns a defensive projection-contract copy.
func (contract ProjectionContract) Clone() ProjectionContract {
	contract.comparedFields = append([]string(nil), contract.comparedFields...)
	return contract
}

func cloneProjectionContract(contract ProjectionContract) ProjectionContract {
	contract.comparedFields = append([]string(nil), contract.comparedFields...)
	return contract
}

func cloneProjectionState(state ProjectionState) ProjectionState {
	state.contract = cloneProjectionContract(state.contract)
	return state
}

func compareProjectionAddress(left ProjectionAddress, right ProjectionAddress) int {
	for _, comparison := range []int{
		strings.Compare(string(left.document.target), string(right.document.target)),
		strings.Compare(string(left.document.scope), string(right.document.scope)),
		strings.Compare(left.document.root.String(), right.document.root.String()),
		strings.Compare(left.placementID, right.placementID),
		strings.Compare(string(left.mergeUnit), string(right.mergeUnit)),
		strings.Compare(string(left.contentPath), string(right.contentPath)),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}
