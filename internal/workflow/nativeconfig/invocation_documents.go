package nativeconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

var (
	ErrInvocationKeyChanged     = errors.New("selected settings changed during host invocation")
	ErrInvocationOutsideChanged = errors.New("host invocation changed settings outside its rewrite area")
)

type invocationKey struct {
	state    durable.ManagedAggregateState
	document aggregate.DocumentAddress
	path     output.KeyPath
	value    configdocument.Value
}

type invocationLayout struct {
	layout  rootedpath.PathLayout
	aliases []aggregate.DocumentAddress
}

type invocationRead struct {
	invocationLayout
	syntax configdocument.Syntax
}

// InvocationDocuments retains key intent, whole-file preservation scopes and
// observation layouts, not mutation authority or an executable command.
type InvocationDocuments struct {
	keys      []invocationKey
	documents []invocationRead
	whole     []invocationLayout
}

type InvocationBaseline struct {
	documents map[aggregate.DocumentAddress]configdocument.Document
	whole     map[aggregate.DocumentAddress]aggregate.Document
}

func PrepareInvocationDocuments(ctx context.Context, paths daempaths.Paths, input readiness.NativeAssessmentInput, sources []lockobserve.NativeSourceObservation) (InvocationDocuments, error) {
	if ctx == nil || input.Documents == nil || input.Budget == nil {
		return InvocationDocuments{}, fmt.Errorf("native invocation documents require retained input and allocation")
	}
	var result InvocationDocuments
	for _, source := range sources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		if placement.Ownership() == hostconfig.OwnershipDocument {
			continue
		}
		for _, projection := range source.Projections() {
			item, present, err := projection.ManagedAggregateContribution()
			if err != nil || !present {
				return InvocationDocuments{}, errors.Join(fmt.Errorf("native invocation requires selected key contributions"), err)
			}
			contribution := item.Contribution()
			name, native := aggregate.HostConfigSyntaxForCodec(contribution.CodecContractID())
			if !native {
				return InvocationDocuments{}, fmt.Errorf("invocation key is not a native contribution")
			}
			syntax, err := configdocument.ParseSyntax(name)
			if err != nil {
				return InvocationDocuments{}, err
			}
			value, err := configdocument.DecodeValue(ctx, syntax, contribution.CanonicalContribution(), DocumentLimits())
			if err != nil {
				return InvocationDocuments{}, err
			}
			state, err := durable.NewManagedAggregateState(projection.SubjectID(), contribution)
			if err != nil {
				return InvocationDocuments{}, err
			}
			result.keys = append(result.keys, invocationKey{state: state, document: contribution.Contract().Address().Document(), path: contribution.Address().KeyPath(), value: value})
		}
	}
	for _, namespace := range input.Documents.Evidence().Namespaces() {
		aliases := namespace.Addresses()
		path, err := managedhostpath.Resolver(paths).Resolve(aliases[0].AggregateRoot())
		if err != nil {
			return InvocationDocuments{}, err
		}
		layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, physicalwork.MaximumPhysicalPathDepth, input.Budget)
		if err != nil {
			return InvocationDocuments{}, err
		}
		selected := invocationLayout{layout: layout, aliases: aliases}
		if IsWholeInvocationDocument(aliases, sources, input.Documents.Evidence().BeforeState()) {
			result.whole = append(result.whole, selected)
			continue
		}
		syntax, err := invocationSyntax(aliases, sources, input.Documents.Evidence().BeforeState())
		if err != nil {
			return InvocationDocuments{}, err
		}
		result.documents = append(result.documents, invocationRead{invocationLayout: selected, syntax: syntax})
	}
	return result, ctx.Err()
}

// IsWholeInvocationDocument selects preservation after positive keys settle.
// Nonempty narrowing reads keys; empty narrowing retains whole preservation
// until its final ownership retirement. This does not prove prefix settlement.
func IsWholeInvocationDocument(aliases []aggregate.DocumentAddress, sources []lockobserve.NativeSourceObservation, before durable.Snapshot) bool {
	positiveKeys := false
	for _, source := range sources {
		placement, present := source.SourceContract().HostConfigPlacement()
		if !present || !slices.ContainsFunc(aliases, func(alias aggregate.DocumentAddress) bool {
			return alias.Target() == placement.Target() && alias.Scope() == placement.Scope() && alias.AggregateRoot() == placement.Destination()
		}) {
			continue
		}
		if placement.Ownership() == hostconfig.OwnershipDocument {
			return true
		}
		positiveKeys = positiveKeys || len(source.Projections()) != 0
	}
	if positiveKeys {
		return false
	}
	for _, state := range before.ManagedPaths() {
		if aggregate.IsHostConfigDocumentSubject(state.Subject()) && slices.ContainsFunc(aliases, func(alias aggregate.DocumentAddress) bool {
			return slices.Contains(state.ConsumerTargets(), alias.Target()) && alias.Scope() == state.Scope() && alias.AggregateRoot() == state.Destination()
		}) {
			return true
		}
	}
	return false
}

func invocationSyntax(aliases []aggregate.DocumentAddress, sources []lockobserve.NativeSourceObservation, before durable.Snapshot) (configdocument.Syntax, error) {
	for _, source := range sources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		for _, alias := range aliases {
			if alias.Target() == placement.Target() && alias.Scope() == placement.Scope() && alias.AggregateRoot() == placement.Destination() {
				return configdocument.ParseSyntax(string(placement.Grammar()))
			}
		}
	}
	for _, state := range before.ManagedAggregates() {
		name, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().CodecContractID())
		if native && slices.Contains(aliases, state.Contribution().Contract().Address().Document()) {
			return configdocument.ParseSyntax(name)
		}
	}
	return 0, fmt.Errorf("native invocation document has no selected syntax")
}

// RequireManagedState checks prior settlement without granting ownership or
// interpreting an externally matching value as an Apply completion.
func (documents InvocationDocuments) RequireManagedState(input readiness.NativeAssessmentInput, sources []lockobserve.NativeSourceObservation) error {
	if input.Documents == nil {
		return fmt.Errorf("refresh requires its retained settings observation")
	}
	before := input.Documents.Evidence().BeforeState()

	states := make(map[topology.SubjectID]durable.ManagedAggregateState)
	for _, state := range before.ManagedAggregates() {
		states[state.Subject()] = state
	}
	for _, key := range documents.keys {
		state, present := states[key.state.Subject()]
		if !present || !key.state.Equal(state) {
			return fmt.Errorf("selected settings require apply before refresh")
		}
	}

	whole := make(map[topology.SubjectID]durable.ManagedPathState)
	for _, state := range before.ManagedPaths() {
		if !aggregate.IsHostConfigDocumentSubject(state.Subject()) {
			continue
		}
		whole[state.Subject()] = state
		for _, consumer := range state.ConsumerTargets() {
			alias, err := aggregate.NewDocumentAddress(consumer, state.Scope(), state.Destination())
			if err != nil {
				return err
			}
			document, mode, observed := input.Documents.Evidence().Document(alias)
			if !observed || !document.Exists() || mode == nil || mode.FileMode() != state.FileMode() || artifact.HashFileContent(document.Content()) != state.ContentHash() {
				return fmt.Errorf("whole-file settings require apply before refresh")
			}
		}
	}

	for _, source := range sources {
		desired, entire, err := source.DesiredWholeState()
		if err != nil {
			return err
		}
		if entire {
			if current, present := whole[desired.Subject()]; !present || !current.Equal(desired) {
				return fmt.Errorf("whole-file settings require apply before refresh")
			}
			continue
		}
		for _, state := range whole {
			owner, _ := topologyprojection.EntityID(state.Subject())
			if owner == source.SourceContract().EntityID() {
				return fmt.Errorf("settings ownership changes require apply before refresh")
			}
		}
	}
	return nil
}

func (documents InvocationDocuments) ReserveReads(budget *physicalwork.Budget, extra physicalwork.InvocationWork) (physicalwork.InvocationWorkReservation, error) {
	var work invocationReadWork
	layouts := slices.Clone(documents.whole)
	for _, document := range documents.documents {
		layouts = append(layouts, document.invocationLayout)
	}
	for range 2 {
		for _, document := range layouts {
			if err := document.layout.ReserveDestinationCapture(physicalwork.MaximumPhysicalPathDepth, &work); err != nil {
				return physicalwork.InvocationWorkReservation{}, err
			}
			if err := document.layout.ReserveDestinationAccess(physicalwork.MaximumPhysicalPathDepth, &work); err != nil {
				return physicalwork.InvocationWorkReservation{}, err
			}
			work.bytes += DocumentLimits().MaximumBytes
		}
	}
	content, err := workmeasure.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return physicalwork.InvocationWorkReservation{}, err
	}
	compiled, err := physicalwork.NewInvocationWork(work.paths, content)
	if err != nil {
		return physicalwork.InvocationWorkReservation{}, err
	}
	compiled, err = compiled.Plus(extra)
	if err != nil {
		return physicalwork.InvocationWorkReservation{}, err
	}
	return budget.ReserveInvocation(compiled, physicalwork.InvocationWork{})
}

type invocationReadWork struct {
	paths   []int
	entries int
	bytes   int64
}

func (work *invocationReadWork) AdmitPathComponents(count int) error {
	work.paths = append(work.paths, count)
	return nil
}

func (work *invocationReadWork) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	work.paths = append(work.paths, paths)
	work.entries += entries
	work.bytes += bytes
	return nil
}

func (documents InvocationDocuments) Read(ctx context.Context, budget *physicalwork.Budget) (InvocationBaseline, error) {
	result := InvocationBaseline{documents: make(map[aggregate.DocumentAddress]configdocument.Document), whole: make(map[aggregate.DocumentAddress]aggregate.Document)}
	for _, selected := range documents.documents {
		document, err := selected.read(ctx, budget)
		if err != nil {
			return InvocationBaseline{}, err
		}
		for _, alias := range selected.aliases {
			result.documents[alias] = document
		}
	}
	for _, selected := range documents.whole {
		document, err := selected.read(ctx, budget)
		if err != nil {
			return InvocationBaseline{}, err
		}
		for _, alias := range selected.aliases {
			result.whole[alias] = document
		}
	}
	return result, ctx.Err()
}

func (selected invocationRead) read(ctx context.Context, budget *physicalwork.Budget) (configdocument.Document, error) {
	document, err := selected.invocationLayout.read(ctx, budget)
	if err != nil {
		return configdocument.Document{}, err
	}
	if !document.Exists() {
		return configdocument.Absent(selected.syntax, DocumentLimits())
	}
	return configdocument.Parse(ctx, document.Content(), selected.syntax, DocumentLimits())
}

func (selected invocationLayout) read(ctx context.Context, budget *physicalwork.Budget) (document aggregate.Document, returnErr error) {
	root, destination, err := selected.layout.CaptureDestinationBounded(physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	content, _, _, readErr := storagecommit.ReadRootedRegularFileUpTo(ctx, capability, DocumentLimits().MaximumBytes)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = DocumentLimits().MaximumBytes
	}
	work, err := workmeasure.NewArtifactWork(0, charged)
	if err == nil {
		err = budget.AdmitTree(work)
	}
	if err := errors.Join(closeErr, err, ctx.Err()); err != nil {
		return document, err
	}
	if errors.Is(readErr, fs.ErrNotExist) {
		return aggregate.AbsentDocument(), nil
	}
	if readErr != nil {
		return document, readErr
	}
	return aggregate.ExistingDocument(content), nil
}

func (documents InvocationDocuments) Qualify(ctx context.Context, before, after InvocationBaseline, rewrites relationhost.NativeSettingsRewrites) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(before.whole) != len(after.whole) {
		return fmt.Errorf("%w: whole-file membership changed", ErrInvocationOutsideChanged)
	}
	for _, selected := range documents.whole {
		for _, alias := range selected.aliases {
			if _, present := before.whole[alias]; !present {
				return fmt.Errorf("%w: whole file was not observed", ErrInvocationOutsideChanged)
			}
		}
	}
	for alias, original := range before.whole {
		current, present := after.whole[alias]
		if !present || !original.Equal(current) {
			return fmt.Errorf("%w: whole file %q changed", ErrInvocationOutsideChanged, alias.AggregateRoot())
		}
	}
	for _, selected := range documents.documents {
		for _, alias := range selected.aliases {
			if _, present := before.documents[alias]; !present {
				return fmt.Errorf("%w: expected document was not observed", ErrInvocationOutsideChanged)
			}
			if _, present := after.documents[alias]; !present {
				return fmt.Errorf("%w: expected document was not observed", ErrInvocationOutsideChanged)
			}
		}
	}
	for _, key := range documents.keys {
		for _, observed := range []InvocationBaseline{before, after} {
			document, selected := observed.documents[key.document]
			if !selected {
				return fmt.Errorf("%w: document %q was not observed", ErrInvocationKeyChanged, key.document.AggregateRoot())
			}
			value, present, err := document.Lookup(key.path)
			if err != nil || !present || !value.Equal(key.value) {
				return errors.Join(fmt.Errorf("%w: %s at %q", ErrInvocationKeyChanged, key.path.String(), key.document.AggregateRoot()), err)
			}
		}
	}
	addresses := make([]aggregate.DocumentAddress, 0, len(before.documents))
	for address := range before.documents {
		addresses = append(addresses, address)
	}
	slices.SortFunc(addresses, func(left, right aggregate.DocumentAddress) int {
		return strings.Compare(string(left.Target())+"\x00"+string(left.Scope())+"\x00"+left.AggregateRoot().String(), string(right.Target())+"\x00"+string(right.Scope())+"\x00"+right.AggregateRoot().String())
	})
	if len(before.documents) != len(after.documents) {
		return fmt.Errorf("%w: observed document membership changed", ErrInvocationOutsideChanged)
	}
	for _, address := range addresses {
		current, present := after.documents[address]
		if !present {
			return fmt.Errorf("%w: document %q was not observed", ErrInvocationOutsideChanged, address.AggregateRoot())
		}
		same, err := rewrites.PreservesOutside(ctx, address, before.documents[address], current)
		if err != nil || !same {
			return errors.Join(fmt.Errorf("%w: %q", ErrInvocationOutsideChanged, address.AggregateRoot()), err)
		}
	}
	return nil
}
