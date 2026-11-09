package record

import (
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

type nativeNamespaceFacts struct {
	claims            journaldocument.ClaimSelection
	manifestReference output.RootReference
	roots             map[output.RootReference]rootedpath.AuthorityProvenance
	alternatives      map[output.RootReference]configuration.PointAlternative
}

type documentNamespaceFacts struct {
	writer         journalnamespace.WriterPin
	writerSpelling string
	aliases        []documentAliasFacts
}

type documentAliasFacts struct {
	address  aggregate.DocumentAddress
	location configuration.AliasLocation
	binding  configuration.AliasBinding
	root     rootedpath.AuthorityProvenance
	selected recoveryDocumentObservation
	before   recoveryDocumentObservation
}
