package nativeoperation

import (
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
)

func (operation *operation) registryReferences(captured journalpublication.CapturedNativeJournal) []pathauthority.Reference {
	references := operation.destinations.References()
	if !operation.ownerReference.IsZero() {
		references = append(references, operation.ownerReference)
	}
	return append(references, captured.References()...)
}
