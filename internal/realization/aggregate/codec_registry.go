package aggregate

import (
	"bytes"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// Document is one ephemeral aggregate document supplied to a pure codec.
type Document struct {
	exists  bool
	content []byte
}

// AbsentDocument constructs a missing host document value.
func AbsentDocument() Document { return Document{} }

// ExistingDocument constructs an existing document, including an existing empty file.
func ExistingDocument(content []byte) Document {
	return Document{exists: true, content: bytes.Clone(content)}
}

func (document Document) Exists() bool    { return document.exists }
func (document Document) Content() []byte { return bytes.Clone(document.content) }

// Equal compares document presence and bytes.
func (document Document) Equal(other Document) bool {
	return document.exists == other.exists && bytes.Equal(document.content, other.content)
}

// Validate rejects absent documents that carry impossible bytes.
func (document Document) Validate() error {
	if !document.exists && len(document.content) != 0 {
		return fmt.Errorf("absent aggregate document must not carry content")
	}
	return nil
}

func cloneDocument(document Document) Document {
	return Document{exists: document.exists, content: bytes.Clone(document.content)}
}

// Codec is the pure host-aggregate boundary protocol. Implementations must not perform I/O.
type Codec interface {
	ContractID() CodecContractID
	MaximumDocumentBytes() int64
	ValidateContribution(ManagedContribution) error
	Read(Document, Selection) (Snapshot, *CodecFailure)
	Render(Document, Plan) (RenderedDocument, *CodecFailure)
	Restore(Document, Snapshot) (RenderedDocument, *CodecFailure)
}

// OperationPreconditionKind identifies a fresh host fact required before an
// aggregate document may be reconciled or mutated.
type OperationPreconditionKind string

const (
	OperationPreconditionDocumentAbsent OperationPreconditionKind = "document_absent"
)

// OperationPrecondition is one codec-profile-owned host fact. It carries no
// observation result or mutation authority.
type OperationPrecondition struct {
	kind     OperationPreconditionKind
	document DocumentAddress
}

func newOperationPrecondition(
	kind OperationPreconditionKind,
	selectedTarget target.Target,
	scope target.Scope,
	root output.Destination,
) (OperationPrecondition, error) {
	document, err := newDocumentAddress(selectedTarget, scope, root)
	if err != nil {
		return OperationPrecondition{}, err
	}
	precondition := OperationPrecondition{kind: kind, document: document}
	if err := precondition.Validate(); err != nil {
		return OperationPrecondition{}, err
	}
	return precondition, nil
}

// Validate rejects zero or forged operation preconditions.
func (precondition OperationPrecondition) Validate() error {
	if precondition.kind != OperationPreconditionDocumentAbsent {
		return fmt.Errorf("aggregate operation precondition kind %q is unsupported", precondition.kind)
	}
	return precondition.document.Validate()
}

func (precondition OperationPrecondition) Kind() OperationPreconditionKind {
	return precondition.kind
}

func (precondition OperationPrecondition) DocumentAddress() DocumentAddress {
	return precondition.document
}

// UnsatisfiedDetail returns the stable, redaction-safe explanation for a
// failed fresh precondition observation.
func (precondition OperationPrecondition) UnsatisfiedDetail() string {
	switch precondition.kind {
	case OperationPreconditionDocumentAbsent:
		return fmt.Sprintf(
			"aggregate operation precondition %q is not satisfied: unsupported alternate config %q is present",
			precondition.kind,
			precondition.document.AggregateRoot(),
		)
	default:
		return fmt.Sprintf(
			"aggregate operation precondition %q is not satisfied for %q",
			precondition.kind,
			precondition.document.AggregateRoot(),
		)
	}
}

// OperationPreconditionsForContract returns the exact static operation
// preconditions owned by one admitted projection placement.
func OperationPreconditionsForContract(
	contract ProjectionContract,
) ([]OperationPrecondition, bool, error) {
	if err := contract.Validate(); err != nil {
		return nil, false, err
	}
	contractID := contract.CodecContractID()
	if placement, ok := HookPlacementForCodec(contractID); ok {
		if contract.Address().PlacementID() != string(placement.ID()) {
			return nil, false, nil
		}
		return nil, true, nil
	}
	placement, ok := MCPPlacementForID(MCPPlacementID(contract.Address().PlacementID()))
	if !ok || placement.CodecContractID() != contractID {
		return nil, false, nil
	}
	conflictingPath, ok := placement.ConflictingConfigPath()
	if !ok {
		return nil, true, nil
	}
	precondition, err := newOperationPrecondition(
		OperationPreconditionDocumentAbsent,
		placement.Target(),
		placement.Scope(),
		conflictingPath,
	)
	if err != nil {
		return nil, false, err
	}
	return []OperationPrecondition{precondition}, true, nil
}

// OperationPreconditionsForSelection returns one placement's exact static
// preconditions after rejecting cross-placement selections.
func OperationPreconditionsForSelection(
	selection Selection,
) ([]OperationPrecondition, bool, error) {
	contracts := selection.Contracts()
	if len(contracts) == 0 {
		return nil, false, fmt.Errorf("aggregate codec selection is required")
	}
	placementID := contracts[0].Address().PlacementID()
	for _, contract := range contracts[1:] {
		if contract.Address().PlacementID() != placementID {
			return nil, false, fmt.Errorf("aggregate codec selection mixes placements")
		}
	}
	return OperationPreconditionsForContract(contracts[0])
}

func newDocumentAddress(
	selectedTarget target.Target,
	scope target.Scope,
	aggregateRoot output.Destination,
) (DocumentAddress, error) {
	parsedTarget, err := target.ParseTarget(string(selectedTarget))
	if err != nil {
		return DocumentAddress{}, fmt.Errorf("target: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return DocumentAddress{}, fmt.Errorf("scope: %w", err)
	}
	if err := aggregateRoot.ValidateScope(parsedScope); err != nil {
		return DocumentAddress{}, fmt.Errorf("aggregate root: %w", err)
	}
	return DocumentAddress{target: parsedTarget, scope: parsedScope, root: aggregateRoot}, nil
}

// Validate rejects zero or forged aggregate document addresses.
func (address DocumentAddress) Validate() error {
	canonical, err := newDocumentAddress(address.target, address.scope, address.root)
	if err != nil {
		return err
	}
	if canonical != address {
		return fmt.Errorf("aggregate document address is not canonical")
	}
	return nil
}

// ValidateSubjectContract checks that subject is the one admitted topology
// identity represented by contract. It deliberately excludes desired bytes so
// recovery can validate persisted static contracts without reconstructing
// current intent.
func ValidateSubjectContract(subject topology.SubjectID, contract ProjectionContract) error {
	if err := subject.Validate(); err != nil {
		return fmt.Errorf("aggregate subject: %w", err)
	}
	if subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("aggregate contribution requires projection subject")
	}
	if err := contract.Validate(); err != nil {
		return err
	}
	if placement, ok := HookPlacementForCodec(contract.CodecContractID()); ok {
		expected, err := placement.Contribution("subject-contract")
		if err != nil {
			return err
		}
		if !expected.Contract().Equal(contract) {
			return fmt.Errorf("Hook aggregate subject contract differs from its placement")
		}
		id, entityBacked := topologyprojection.EntityID(subject)
		if !entityBacked || id.Kind() != entity.KindHook || subject.Namespace() != string(placement.ID()) {
			return fmt.Errorf("Hook aggregate subject does not match its placement contract")
		}
		return nil
	}

	placement, ok := MCPPlacementForID(MCPPlacementID(contract.Address().PlacementID()))
	if !ok || placement.CodecContractID() != contract.CodecContractID() {
		return fmt.Errorf("aggregate subject contract codec %q is not admitted", contract.CodecContractID())
	}
	serverID, ok := placement.ServerIDFromContentPath(contract.Address().ContentPath())
	if !ok {
		return fmt.Errorf("MCP aggregate subject contract content path is outside its placement")
	}
	expected, err := placement.ProjectionContract(serverID)
	if err != nil {
		return err
	}
	if !expected.Equal(contract) {
		return fmt.Errorf("MCP aggregate subject contract differs from its placement")
	}
	id, err := entity.New(entity.KindMCPServer, serverID)
	if err != nil {
		return err
	}
	expectedSubject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
	if err != nil {
		return err
	}
	if subject != expectedSubject {
		return fmt.Errorf("MCP aggregate subject %q does not match placement server %q", subject, serverID)
	}
	return nil
}
