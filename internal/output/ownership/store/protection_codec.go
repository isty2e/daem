package store

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
)

type pendingProtectionRecord struct {
	StatefileAuthority   pathAuthorityRecord       `json:"statefile_authority"`
	ManifestPath         string                    `json:"manifest_path"`
	InvocationID         string                    `json:"invocation_id"`
	OperationID          string                    `json:"operation_id"`
	RequestFingerprint   string                    `json:"request_fingerprint"`
	SelectionFingerprint string                    `json:"selection_fingerprint"`
	Phase                ownership.ProtectionPhase `json:"phase"`
	Documents            []protectedDocumentRecord `json:"documents"`
	Execution            *executionBindingRecord   `json:"execution,omitempty"`
}

type protectedDocumentRecord struct {
	Bindings []documentBindingRecord `json:"bindings"`
	UserID   *uint32                 `json:"user_id"`
	GroupID  *uint32                 `json:"group_id"`
	ModeCap  *uint32                 `json:"mode_cap"`
}

type executionBindingRecord struct {
	Platform   executionidentity.Platform `json:"platform"`
	Realm      string                     `json:"realm"`
	Epoch      string                     `json:"epoch"`
	Visibility string                     `json:"visibility,omitempty"`
	Group      int64                      `json:"group"`
}

func decodePendingProtections(content json.RawMessage) ([]ownership.PendingProtection, error) {
	if len(content) == 0 {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("pending_protections requires an array")
	}
	var records []json.RawMessage
	if err := decodeRegistryJSON(content, &records); err != nil {
		return nil, err
	}
	protections := make([]ownership.PendingProtection, 0, len(records))
	for index, raw := range records {
		if err := jsonstrict.ValidateRecord(raw, "pending protection", maximumOwnershipRegistryJSONDepth); err != nil {
			return nil, fmt.Errorf("pending_protections[%d]: %w", index, err)
		}
		var record pendingProtectionRecord
		if err := decodeRegistryJSON(raw, &record); err != nil {
			return nil, err
		}
		protection, err := decodePendingProtection(&record)
		if err != nil {
			return nil, fmt.Errorf("pending_protections[%d]: %w", index, err)
		}
		protections = append(protections, protection)
	}
	return protections, nil
}

func decodePendingProtection(record *pendingProtectionRecord) (ownership.PendingProtection, error) {
	if record == nil {
		return ownership.PendingProtection{}, fmt.Errorf("pending protection must not be null")
	}
	statefile, err := pathauthority.NewExact(record.StatefileAuthority.Key, record.StatefileAuthority.Witness)
	if err != nil {
		return ownership.PendingProtection{}, err
	}
	owner, err := stateauthority.New(statefile, record.ManifestPath)
	if err != nil {
		return ownership.PendingProtection{}, err
	}
	invocation, err := executionidentity.NewInvocation(record.InvocationID)
	if err != nil {
		return ownership.PendingProtection{}, err
	}
	correlation, err := ownership.NewProtectionCorrelation(record.OperationID, record.RequestFingerprint, record.SelectionFingerprint)
	if err != nil {
		return ownership.PendingProtection{}, err
	}
	documents := make([]ownership.ProtectedDocument, 0, len(record.Documents))
	for index, document := range record.Documents {
		if document.UserID == nil || document.GroupID == nil || document.ModeCap == nil {
			return ownership.PendingProtection{}, fmt.Errorf("protected document[%d] requires numeric user_id, group_id and mode_cap", index)
		}
		bindings, err := decodeDocumentBindings(document.Bindings)
		if err != nil {
			return ownership.PendingProtection{}, err
		}
		target, err := ownership.NewProtectedDocument(bindings, *document.UserID, *document.GroupID, *document.ModeCap)
		if err != nil {
			return ownership.PendingProtection{}, err
		}
		documents = append(documents, target)
	}
	protection, err := ownership.NewArmedProtection(owner, invocation, correlation, documents)
	if err != nil {
		return ownership.PendingProtection{}, err
	}
	switch record.Phase {
	case ownership.ProtectionArmed:
		if record.Execution != nil {
			return ownership.PendingProtection{}, fmt.Errorf("armed protection must not contain execution identity")
		}
		return protection, nil
	case ownership.ProtectionBound:
		if record.Execution == nil {
			return ownership.PendingProtection{}, fmt.Errorf("bound protection requires execution identity")
		}
		var executionContext executionidentity.Context
		if record.Execution.Visibility == "" {
			executionContext, err = executionidentity.NewContext(record.Execution.Platform, record.Execution.Realm, record.Execution.Epoch)
		} else {
			executionContext, err = executionidentity.NewContextWithVisibility(record.Execution.Platform, record.Execution.Realm, record.Execution.Epoch, record.Execution.Visibility)
		}
		if err != nil {
			return ownership.PendingProtection{}, err
		}
		execution, err := executionidentity.NewBinding(invocation, executionContext, record.Execution.Group)
		if err != nil {
			return ownership.PendingProtection{}, err
		}
		return protection.Bind(execution)
	default:
		return ownership.PendingProtection{}, fmt.Errorf("unsupported pending protection phase %q", record.Phase)
	}
}

func encodePendingProtections(protections []ownership.PendingProtection) (json.RawMessage, error) {
	if len(protections) == 0 {
		return nil, nil
	}
	records := make([]pendingProtectionRecord, 0, len(protections))
	for _, protection := range protections {
		statefile := protection.Owner().StatefileAuthority()
		correlation := protection.Correlation()
		record := pendingProtectionRecord{
			StatefileAuthority: pathAuthorityRecord{Key: statefile.Key(), Witness: statefile.Witness()},
			ManifestPath:       protection.Owner().ManifestPath(), InvocationID: protection.Invocation().String(),
			OperationID: correlation.OperationID(), RequestFingerprint: correlation.RequestFingerprint(),
			SelectionFingerprint: correlation.SelectionFingerprint(), Phase: protection.Phase(),
			Documents: make([]protectedDocumentRecord, 0, len(protection.Documents())),
		}
		for _, document := range protection.Documents() {
			userID, groupID, modeCap := document.UserID(), document.GroupID(), document.ModeCap()
			record.Documents = append(record.Documents, protectedDocumentRecord{
				Bindings: persistedDocumentBindings(document.Bindings()),
				UserID:   &userID, GroupID: &groupID, ModeCap: &modeCap,
			})
		}
		if binding, present := protection.Execution(); present {
			context := binding.Context()
			record.Execution = &executionBindingRecord{
				Platform: context.Platform(), Realm: context.Realm(), Epoch: context.Epoch(), Group: binding.Group(), Visibility: context.Visibility(),
			}
		}
		records = append(records, record)
	}
	content, err := json.Marshal(records)
	if err != nil {
		return nil, fmt.Errorf("encode pending protections: %w", err)
	}
	return content, nil
}
