package ownership

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/executionidentity"
)

// ProtectedDocument freezes one existing document's binding cohort and metadata
// ceiling. It is not a descriptor, a root grant or permission to rewrite bytes.
type ProtectedDocument struct {
	bindings []DocumentBinding
	userID   uint32
	groupID  uint32
	modeCap  uint32
}

func NewProtectedDocument(bindings []DocumentBinding, userID, groupID, modeCap uint32) (ProtectedDocument, error) {
	canonical, _, err := canonicalDocumentBindings(bindings)
	if err != nil {
		return ProtectedDocument{}, err
	}
	if len(canonical) == 0 {
		return ProtectedDocument{}, fmt.Errorf("protected document requires its complete binding cohort")
	}
	physical := canonical[0].PathAuthority()
	for _, binding := range canonical {
		if !binding.PathAuthority().Equal(physical) {
			return ProtectedDocument{}, fmt.Errorf("protected document bindings do not identify one exact namespace")
		}
		if physical.Contains(binding.Owner().StatefileAuthority()) || binding.Owner().StatefileAuthority().Contains(physical) {
			return ProtectedDocument{}, fmt.Errorf("protected document overlaps a cohort state authority")
		}
	}
	if modeCap & ^uint32(0o777) != 0 {
		return ProtectedDocument{}, fmt.Errorf("protected document ceiling must contain only permission bits")
	}
	return ProtectedDocument{bindings: canonical, userID: userID, groupID: groupID, modeCap: modeCap}, nil
}

func (document ProtectedDocument) Validate() error {
	_, err := NewProtectedDocument(document.bindings, document.userID, document.groupID, document.modeCap)
	return err
}

func (document ProtectedDocument) Bindings() []DocumentBinding {
	return slices.Clone(document.bindings)
}

func (document ProtectedDocument) PathAuthority() pathauthority.Exact {
	if len(document.bindings) == 0 {
		return pathauthority.Exact{}
	}
	return document.bindings[0].PathAuthority()
}

func (document ProtectedDocument) UserID() uint32  { return document.userID }
func (document ProtectedDocument) GroupID() uint32 { return document.groupID }
func (document ProtectedDocument) ModeCap() uint32 { return document.modeCap }

// Restriction derives only the removal of newly granted access. The caller
// must separately qualify the regular file, ACL, namespace and same descriptor.
func (document ProtectedDocument) Restriction(userID, groupID, observedMode uint32) (uint32, bool, error) {
	if err := document.Validate(); err != nil {
		return 0, false, err
	}
	if document.userID != userID || document.groupID != groupID {
		return 0, false, &ProtectionRefusal{kind: ProtectionOwnerChanged}
	}
	if observedMode & ^uint32(0o777) != 0 || observedMode&0o700 != document.modeCap&0o700 || observedMode&0o111 != document.modeCap&0o111 {
		return 0, false, &ProtectionRefusal{kind: ProtectionModeChanged}
	}
	restricted := observedMode & document.modeCap
	return restricted, restricted != observedMode, nil
}

func (document ProtectedDocument) Equal(other ProtectedDocument) bool {
	return document.userID == other.userID && document.groupID == other.groupID && document.modeCap == other.modeCap &&
		slices.EqualFunc(document.bindings, other.bindings, DocumentBinding.Equal)
}

// ProtectionCorrelation records the admitted operation and exact request/plan
// fingerprints. Diagnostic attempt history is not this safety authority.
type ProtectionCorrelation struct {
	operationID          string
	requestFingerprint   string
	selectionFingerprint string
}

func NewProtectionCorrelation(operationID, requestFingerprint, selectionFingerprint string) (ProtectionCorrelation, error) {
	if err := ValidateOperationID(operationID); err != nil {
		return ProtectionCorrelation{}, err
	}
	for _, fingerprint := range []string{requestFingerprint, selectionFingerprint} {
		if len(fingerprint) != 64 || strings.ToLower(fingerprint) != fingerprint {
			return ProtectionCorrelation{}, fmt.Errorf("protection correlation requires lowercase SHA-256 fingerprints")
		}
		if _, err := hex.DecodeString(fingerprint); err != nil {
			return ProtectionCorrelation{}, fmt.Errorf("protection correlation fingerprint: %w", err)
		}
	}
	return ProtectionCorrelation{operationID, requestFingerprint, selectionFingerprint}, nil
}

func (correlation ProtectionCorrelation) Validate() error {
	_, err := NewProtectionCorrelation(correlation.operationID, correlation.requestFingerprint, correlation.selectionFingerprint)
	return err
}

func (correlation ProtectionCorrelation) OperationID() string { return correlation.operationID }
func (correlation ProtectionCorrelation) RequestFingerprint() string {
	return correlation.requestFingerprint
}

func (correlation ProtectionCorrelation) SelectionFingerprint() string {
	return correlation.selectionFingerprint
}

type ProtectionPhase string

const (
	ProtectionArmed ProtectionPhase = "armed"
	ProtectionBound ProtectionPhase = "bound"
)

// PendingProtection remains a document-wide write fence until exact settlement.
// Its finite target set is also the immutable permission-cleanup demand.
type PendingProtection struct {
	owner       stateauthority.Authority
	invocation  executionidentity.Invocation
	correlation ProtectionCorrelation
	documents   []ProtectedDocument
	execution   *executionidentity.Binding
}

func NewArmedProtection(owner stateauthority.Authority, invocation executionidentity.Invocation, correlation ProtectionCorrelation, documents []ProtectedDocument) (PendingProtection, error) {
	if err := owner.Validate(); err != nil {
		return PendingProtection{}, err
	}
	if err := invocation.Validate(); err != nil {
		return PendingProtection{}, err
	}
	if err := correlation.Validate(); err != nil {
		return PendingProtection{}, err
	}
	if len(documents) == 0 {
		return PendingProtection{}, fmt.Errorf("pending protection requires at least one existing document")
	}
	canonical := slices.Clone(documents)
	for index, document := range canonical {
		if err := document.Validate(); err != nil {
			return PendingProtection{}, fmt.Errorf("protected document[%d]: %w", index, err)
		}
		owned := false
		for _, binding := range document.bindings {
			owned = owned || binding.Owner().Equal(owner)
		}
		if !owned {
			return PendingProtection{}, fmt.Errorf("protected document has no binding for its protection owner")
		}
	}
	slices.SortFunc(canonical, func(left, right ProtectedDocument) int {
		return left.PathAuthority().Compare(right.PathAuthority())
	})
	addresses := make([]ManagedAddress, len(canonical))
	for index, document := range canonical {
		addresses[index], _ = NewManagedAddressFromLegacy(document.PathAuthority(), "")
	}
	if _, _, overlap := firstOverlappingAddress(addresses); overlap {
		return PendingProtection{}, fmt.Errorf("pending protection repeats or overlaps a physical document")
	}
	return PendingProtection{owner: owner, invocation: invocation, correlation: correlation, documents: canonical}, nil
}

func (protection PendingProtection) Validate() error {
	if _, err := NewArmedProtection(protection.owner, protection.invocation, protection.correlation, protection.documents); err != nil {
		return err
	}
	if protection.execution != nil {
		if err := protection.execution.Validate(); err != nil {
			return err
		}
		if protection.execution.Invocation() != protection.invocation {
			return fmt.Errorf("protection execution belongs to another invocation")
		}
	}
	return nil
}

func (protection PendingProtection) Bind(execution executionidentity.Binding) (PendingProtection, error) {
	if err := protection.Validate(); err != nil {
		return PendingProtection{}, err
	}
	if protection.execution != nil {
		return PendingProtection{}, fmt.Errorf("pending protection is already bound")
	}
	if err := execution.Validate(); err != nil {
		return PendingProtection{}, err
	}
	if execution.Invocation() != protection.invocation {
		return PendingProtection{}, fmt.Errorf("protection execution belongs to another invocation")
	}
	protection.execution = &execution
	return protection, nil
}

func (protection PendingProtection) Owner() stateauthority.Authority { return protection.owner }
func (protection PendingProtection) Invocation() executionidentity.Invocation {
	return protection.invocation
}

func (protection PendingProtection) Correlation() ProtectionCorrelation {
	return protection.correlation
}

func (protection PendingProtection) Documents() []ProtectedDocument {
	return slices.Clone(protection.documents)
}

func (protection PendingProtection) Execution() (executionidentity.Binding, bool) {
	if protection.execution == nil {
		return executionidentity.Binding{}, false
	}
	return *protection.execution, true
}

func (protection PendingProtection) Phase() ProtectionPhase {
	if protection.execution != nil {
		return ProtectionBound
	}
	return ProtectionArmed
}

func (protection PendingProtection) Equal(other PendingProtection) bool {
	if !protection.owner.ExactEqual(other.owner) || protection.invocation != other.invocation ||
		protection.correlation != other.correlation || !slices.EqualFunc(protection.documents, other.documents, ProtectedDocument.Equal) {
		return false
	}
	left, leftPresent := protection.Execution()
	right, rightPresent := other.Execution()
	return leftPresent == rightPresent && left == right
}

func protectionPathsOverlap(left, right pathauthority.Exact) bool {
	return left.Contains(right) || right.Contains(left)
}
