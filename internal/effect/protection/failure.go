package protection

type FailureKind string

const (
	ResolutionContextUnqualified FailureKind = "binding_resolution_context_unqualified"
	DocumentNamespaceUnqualified FailureKind = "protected_namespace_unqualified"
	MetadataUnqualified          FailureKind = "protected_metadata_unqualified"
	RegistryUnqualified          FailureKind = "protection_registry_unqualified"
	PublicationFailed            FailureKind = "protection_publication_failed"
)

type Failure struct {
	kind  FailureKind
	cause error
}

func (failure *Failure) Kind() FailureKind { return failure.kind }
func (failure *Failure) Error() string     { return string(failure.kind) }
func (failure *Failure) Unwrap() error     { return failure.cause }

func Refusal(kind FailureKind, cause error) error {
	if cause == nil {
		return nil
	}
	return &Failure{kind: kind, cause: cause}
}
