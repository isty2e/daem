package record

import (
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

type nativeNamespaceFacts struct {
	manifestReference output.RootReference
	roots             map[output.RootReference]rootedpath.AuthorityProvenance
	documents         []documentNamespaceFacts
}

type documentNamespaceFacts struct {
	writer  journalnamespace.WriterPin
	aliases []documentAliasFacts
}

type documentAliasFacts struct {
	address  aggregate.DocumentAddress
	location configuration.AliasLocation
	root     rootedpath.AuthorityProvenance
	selected recoveryDocumentObservation
	before   recoveryDocumentObservation
}
