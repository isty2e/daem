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

// AgentConfigCodecForSyntax selects a native value grammar, not host admission
// or evidence that a concrete codec has been composed into an operation.
func AgentConfigCodecForSyntax(syntax string) (CodecContractID, bool) {
	switch syntax {
	case "json", "jsonc", "json-line-comments", "toml":
		return CodecContractID("native-" + syntax + "-v1"), true
	default:
		return "", false
	}
}

func AgentConfigSyntaxForCodec(id CodecContractID) (string, bool) {
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

func AgentConfigPlacementID(selected target.Target, scope target.Scope) (string, error) {
	if _, err := target.ParseTarget(string(selected)); err != nil {
		return "", err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-agent-config-%s-v1", selected, scope), nil
}

// AgentConfigDocumentSubject binds whole-file ownership to the complete canonical
// declaration, target and scope identity, not only the declaration's entity ID.
func AgentConfigDocumentSubject(id entity.ID, selected target.Target, scope target.Scope) (topology.SubjectID, error) {
	if id.Kind() != entity.KindAgentConfig {
		return topology.SubjectID{}, fmt.Errorf("native document subject requires an agent_config entity")
	}
	placementID, err := AgentConfigPlacementID(selected, scope)
	if err != nil {
		return topology.SubjectID{}, err
	}
	return topologyprojection.Subject(id, placementID)
}

// IsAgentConfigDocumentSubject classifies entity-backed whole-document subjects,
// without validating their placement or granting referent access.
func IsAgentConfigDocumentSubject(subject topology.SubjectID) bool {
	id, present := topologyprojection.EntityID(subject)
	return present && id.Kind() == entity.KindAgentConfig
}

// NewAgentConfigProjectionContract owns native projection shape only. Host
// profiles separately admit the logical destination, grammar and reservations.
func NewAgentConfigProjectionContract(selected target.Target, scope target.Scope, root output.Destination, footprint output.Footprint, codec CodecContractID) (ProjectionContract, error) {
	placementID, err := AgentConfigPlacementID(selected, scope)
	if err != nil {
		return ProjectionContract{}, err
	}
	if _, ok := AgentConfigSyntaxForCodec(codec); !ok {
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

func validateAgentConfigSubjectContract(subject topology.SubjectID, contract ProjectionContract) error {
	address := contract.Address()
	id, key, ok := topologyprojection.AgentConfigKeyIdentity(subject)
	if !ok || key != address.KeyPath() {
		return fmt.Errorf("native key subject does not match its projection scope")
	}
	expectedSubject, err := topologyprojection.AgentConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), key)
	if err != nil || expectedSubject != subject {
		return fmt.Errorf("native key subject does not match its target and scope")
	}
	expected, err := NewAgentConfigProjectionContract(
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
