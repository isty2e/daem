package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const PreserveSiblingsNativeValuesComments SiblingPreservation = "native_values_and_comments"

// HostConfigCodecForSyntax selects a native value grammar, not host admission
// or evidence that a concrete codec has been composed into an operation.
func HostConfigCodecForSyntax(syntax string) (CodecContractID, bool) {
	switch syntax {
	case "json", "jsonc", "json-line-comments", "toml":
		return CodecContractID("native-" + syntax + "-v1"), true
	default:
		return "", false
	}
}

func HostConfigSyntaxForCodec(id CodecContractID) (string, bool) {
	switch id {
	case "native-json-v1":
		return "json", true
	case "native-jsonc-v1":
		return "jsonc", true
	case "native-json-line-comments-v1":
		return "json-line-comments", true
	case "native-toml-v1":
		return "toml", true
	default:
		return "", false
	}
}

func HostConfigPlacementID(selected target.Target, scope target.Scope) (string, error) {
	if selected != target.TargetPi && selected != target.TargetCodex {
		return "", fmt.Errorf("native key projection target is unsupported")
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-host-config-%s-v1", selected, scope), nil
}

// HostConfigDocumentSubject binds whole-file ownership to the complete canonical
// declaration, target and scope identity, not only the declaration's entity ID.
func HostConfigDocumentSubject(id entity.ID, selected target.Target, scope target.Scope) (topology.SubjectID, error) {
	if id.Kind() != entity.KindHostConfig {
		return topology.SubjectID{}, fmt.Errorf("native document subject requires a host-config entity")
	}
	placementID, err := HostConfigPlacementID(selected, scope)
	if err != nil {
		return topology.SubjectID{}, err
	}
	return topologyprojection.Subject(id, placementID)
}

// IsHostConfigDocumentSubject classifies entity-backed whole-document subjects,
// without validating their placement or granting referent access.
func IsHostConfigDocumentSubject(subject topology.SubjectID) bool {
	id, present := topologyprojection.EntityID(subject)
	return present && id.Kind() == entity.KindHostConfig
}

// NewHostConfigProjectionContract owns native projection shape only. Host
// profiles separately admit the logical destination, grammar and reservations.
func NewHostConfigProjectionContract(selected target.Target, scope target.Scope, root output.Destination, footprint output.Footprint, codec CodecContractID) (ProjectionContract, error) {
	placementID, err := HostConfigPlacementID(selected, scope)
	if err != nil {
		return ProjectionContract{}, err
	}
	if _, ok := HostConfigSyntaxForCodec(codec); !ok {
		return ProjectionContract{}, fmt.Errorf("native key projection codec is unsupported")
	}
	return NewProjectionContract(ProjectionContractInput{
		PlacementID: placementID, Target: selected, Scope: scope,
		AggregateRoot: root, Footprint: footprint, MergeUnit: "native-key",
		Cardinality: ContributionExclusive, SiblingRetention: PreserveUnmanagedSiblings,
		SiblingPreservation: PreserveSiblingsNativeValuesComments,
		Equivalence:         EquivalenceCanonicalSemantic, CodecContractID: codec,
		ComparedFields: []string{"value"},
	})
}

func validateHostConfigSubjectContract(subject topology.SubjectID, contract ProjectionContract) error {
	address := contract.Address()
	id, key, ok := topologyprojection.HostConfigKeyIdentity(subject)
	if !ok || key != address.KeyPath() {
		return fmt.Errorf("native key subject does not match its projection scope")
	}
	expectedSubject, err := topologyprojection.HostConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), key)
	if err != nil || expectedSubject != subject {
		return fmt.Errorf("native key subject does not match its target and scope")
	}
	expected, err := NewHostConfigProjectionContract(
		address.Document().Target(), address.Document().Scope(),
		address.Document().AggregateRoot(), address.Footprint(), contract.CodecContractID(),
	)
	if err != nil {
		return err
	}
	if !expected.Equal(contract) {
		return fmt.Errorf("native key subject contract differs from its canonical shape")
	}
	return nil
}
