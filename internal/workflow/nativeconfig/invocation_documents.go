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
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
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

type invocationRead struct {
	layout  rootedpath.PathLayout
	aliases []aggregate.DocumentAddress
	syntax  configdocument.Syntax
}

// InvocationDocuments retains selected key intent and observation layouts, not
// mutation authority or an executable command.
type InvocationDocuments struct {
	keys      []invocationKey
	documents []invocationRead
}

type InvocationBaseline struct {
	documents map[aggregate.DocumentAddress]configdocument.Document
}

// InvocationRewrite is the exact profile-owned rewrite area for one command.
// Documents outside these aliases retain the ordinary complete-content check.
type InvocationRewrite struct {
	Aliases  []aggregate.DocumentAddress
	Preserve func(context.Context, configdocument.Document, configdocument.Document) (bool, error)
}

func PrepareInvocationDocuments(ctx context.Context, paths daempaths.Paths, input readiness.NativeAssessmentInput, sources []lockobserve.NativeSourceObservation) (InvocationDocuments, error) {
	if ctx == nil || input.Documents == nil || input.Budget == nil {
		return InvocationDocuments{}, fmt.Errorf("native invocation documents require retained input and allocation")
	}
	var result InvocationDocuments
	for _, source := range sources {
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
	for _, namespace := range input.Documents.Namespaces() {
		aliases := namespace.Addresses()
		path, err := managedhostpath.Resolver(paths).Resolve(aliases[0].AggregateRoot())
		if err != nil {
			return InvocationDocuments{}, err
		}
		layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, recovery.MaximumPhysicalPathDepth, input.Budget)
		if err != nil {
			return InvocationDocuments{}, err
		}
		syntax, err := invocationSyntax(aliases, sources, input.Documents.BeforeState())
		if err != nil {
			return InvocationDocuments{}, err
		}
		result.documents = append(result.documents, invocationRead{layout: layout, aliases: aliases, syntax: syntax})
	}
	return result, ctx.Err()
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
func (documents InvocationDocuments) RequireManagedState(before durable.Snapshot) error {
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
	return nil
}

func (documents InvocationDocuments) ReserveReads(budget *recovery.PhysicalWorkBudget, extra recovery.InvocationWork) (recovery.InvocationWorkReservation, error) {
	var work invocationReadWork
	for range 2 {
		for _, document := range documents.documents {
			if err := document.layout.ReserveDestinationCapture(recovery.MaximumPhysicalPathDepth, &work); err != nil {
				return recovery.InvocationWorkReservation{}, err
			}
			if err := document.layout.ReserveDestinationAccess(recovery.MaximumPhysicalPathDepth, &work); err != nil {
				return recovery.InvocationWorkReservation{}, err
			}
			work.bytes += DocumentLimits().MaximumBytes
		}
	}
	content, err := recovery.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return recovery.InvocationWorkReservation{}, err
	}
	compiled, err := recovery.NewInvocationWork(work.paths, content)
	if err != nil {
		return recovery.InvocationWorkReservation{}, err
	}
	compiled, err = compiled.Plus(extra)
	if err != nil {
		return recovery.InvocationWorkReservation{}, err
	}
	return budget.ReserveInvocation(compiled, recovery.InvocationWork{})
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

func (documents InvocationDocuments) Read(ctx context.Context, budget *recovery.PhysicalWorkBudget) (InvocationBaseline, error) {
	result := InvocationBaseline{documents: make(map[aggregate.DocumentAddress]configdocument.Document)}
	for _, selected := range documents.documents {
		document, err := selected.read(ctx, budget)
		if err != nil {
			return InvocationBaseline{}, err
		}
		for _, alias := range selected.aliases {
			result.documents[alias] = document
		}
	}
	return result, ctx.Err()
}

func (selected invocationRead) read(ctx context.Context, budget *recovery.PhysicalWorkBudget) (document configdocument.Document, returnErr error) {
	root, destination, err := selected.layout.CaptureDestinationBounded(recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	content, _, _, readErr := storagecommit.ReadRootedRegularFileUpTo(ctx, capability, DocumentLimits().MaximumBytes)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = DocumentLimits().MaximumBytes
	}
	work, err := recovery.NewArtifactWork(0, charged)
	if err == nil {
		err = budget.AdmitTree(work)
	}
	if err := errors.Join(closeErr, err, ctx.Err()); err != nil {
		return document, err
	}
	if errors.Is(readErr, fs.ErrNotExist) {
		return configdocument.Absent(selected.syntax, DocumentLimits())
	}
	if readErr != nil {
		return document, readErr
	}
	return configdocument.Parse(ctx, content, selected.syntax, DocumentLimits())
}

func (documents InvocationDocuments) Qualify(ctx context.Context, before, after InvocationBaseline, rewrites []InvocationRewrite) error {
	if err := ctx.Err(); err != nil {
		return err
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
		preserve := func(ctx context.Context, old, current configdocument.Document) (bool, error) {
			return old.PreservesOutside(ctx, current, nil, nil)
		}
		for _, rewrite := range rewrites {
			if rewrite.Preserve != nil && slices.Contains(rewrite.Aliases, address) {
				preserve = rewrite.Preserve
				break
			}
		}
		same, err := preserve(ctx, before.documents[address], current)
		if err != nil || !same {
			return errors.Join(fmt.Errorf("%w: %q", ErrInvocationOutsideChanged, address.AggregateRoot()), err)
		}
	}
	return nil
}
