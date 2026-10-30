package ownership

type ProtectionRefusalKind string

const (
	ProtectionRecordChanged        ProtectionRefusalKind = "protection_record_changed"
	ProtectionCohortChanged        ProtectionRefusalKind = "protection_cohort_changed"
	ProtectionDocumentOutsideScope ProtectionRefusalKind = "protection_document_outside_scope"
	ProtectionOwnerChanged         ProtectionRefusalKind = "protected_owner_changed"
	ProtectionModeChanged          ProtectionRefusalKind = "protected_mode_changed"
)

type ProtectionRefusal struct {
	kind  ProtectionRefusalKind
	cause error
}

func (failure *ProtectionRefusal) Kind() ProtectionRefusalKind { return failure.kind }
func (failure *ProtectionRefusal) Unwrap() error               { return failure.cause }
func (failure *ProtectionRefusal) Error() string {
	switch failure.kind {
	case ProtectionRecordChanged:
		return "pending protection changed"
	case ProtectionCohortChanged:
		return "protected document binding cohort changed"
	case ProtectionDocumentOutsideScope:
		return "document is outside the frozen protection"
	case ProtectionOwnerChanged:
		return "protected document owner or group changed"
	case ProtectionModeChanged:
		return "protected document owner or non-access mode bits changed"
	default:
		return "permission protection is unqualified"
	}
}
