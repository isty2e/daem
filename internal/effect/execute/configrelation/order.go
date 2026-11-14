package configrelation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

// OrderSequenceEventKind identifies one physical-sequence execution transition.
type OrderSequenceEventKind string

const (
	OrderSequenceStarted OrderSequenceEventKind = "started"
	OrderSequenceDone    OrderSequenceEventKind = "done"
	OrderSequenceFailed  OrderSequenceEventKind = "failed"
)

// OrderSequenceEvent is one exact physical-sequence mutation outcome.
type OrderSequenceEvent struct {
	Kind       OrderSequenceEventKind
	SequenceID hostrelation.PhysicalSequenceID
	Changed    bool
	Err        error
}

// OrderSequenceEventSink observes physical-sequence execution. Nil is a no-op.
type OrderSequenceEventSink func(OrderSequenceEvent)

func (sink OrderSequenceEventSink) emit(event OrderSequenceEvent) {
	if sink != nil {
		sink(event)
	}
}

type selectedOrderDocument struct {
	edit      relationhost.SelectedDocumentEdit
	completed relationhost.VerifiedSelectedImage
}

// OrderPlan owns independently publishable documents, not an atomic class write.
type OrderPlan struct {
	documents []selectedOrderDocument
}

func NewOrderPlan(ctx context.Context, edits []relationhost.SelectedDocumentEdit) (OrderPlan, error) {
	if len(edits) == 0 {
		return OrderPlan{}, fmt.Errorf("extension order requires its selected document edits")
	}
	plan := OrderPlan{documents: make([]selectedOrderDocument, 0, len(edits))}
	paths := make(map[string]bool, len(edits))
	for _, edit := range edits {
		if paths[edit.SelectedPath()] {
			return OrderPlan{}, fmt.Errorf("extension order requires one selected edit per document path")
		}
		paths[edit.SelectedPath()] = true
		limits := configdocument.Limits{MaximumBytes: edit.MaximumBytes(), MaximumDepth: max(1, int(edit.Original().ContentSize()))}
		completed, err := relationhost.CompleteSelectedImage(ctx, edit.Original(), limits, nil, []relationhost.SelectedDocumentEdit{edit})
		if err != nil {
			return OrderPlan{}, err
		}
		plan.documents = append(plan.documents, selectedOrderDocument{edit: edit, completed: completed})
	}
	return plan, nil
}

// PhysicalAuthority returns every exact path this document set may mutate.
func (plan OrderPlan) PhysicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	if len(plan.documents) == 0 {
		return mutation.PhysicalAuthoritySet{}, fmt.Errorf("extension order plan is incomplete")
	}
	requests := make([]mutation.PhysicalAuthorityRequest, 0, len(plan.documents))
	for _, document := range plan.documents {
		if err := document.edit.Validate(); err != nil {
			return mutation.PhysicalAuthoritySet{}, err
		}
		path := document.edit.SelectedPath()
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return mutation.PhysicalAuthoritySet{}, fmt.Errorf("extension order document path is not canonical")
		}
		address := document.edit.DocumentAddress()
		requests = append(requests, mutation.PhysicalAuthorityRequest{Path: path, Target: string(address.Target()), Scope: string(address.Scope())})
	}
	return mutation.NewPhysicalAuthoritySet(requests...)
}

// Bind captures exact rooted entry authority for every selected document.
func (plan OrderPlan) Bind(
	selectedRoot *rootedpath.CapturedRoot,
	selectedRootPath string,
) (*BoundOrder, error) {
	if _, err := plan.PhysicalAuthority(); err != nil {
		return nil, err
	}
	order := &BoundOrder{plan: plan}
	for _, document := range plan.documents {
		authority, err := rootedpath.BindSelectedEntryAuthority(selectedRoot, selectedRootPath, document.edit.SelectedPath())
		if err != nil {
			return nil, errors.Join(err, order.Close())
		}
		order.authorities = append(order.authorities, authority)
	}
	return order, nil
}

type BoundOrder struct {
	plan        OrderPlan
	authorities []*rootedpath.EntryAuthority
	closed      bool
}

// Execute preserves completed earlier documents when a later publication fails.
func (order *BoundOrder) Execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	events OrderSequenceEventSink,
) (int, error) {
	if order == nil || order.closed || ctx == nil || filesystem == nil || len(order.plan.documents) == 0 || len(order.authorities) != len(order.plan.documents) {
		return 0, fmt.Errorf("bound extension order is unavailable")
	}
	changedDocuments := 0
	for index, document := range order.plan.documents {
		sequenceID := document.edit.Sequence().SequenceID()
		events.emit(OrderSequenceEvent{Kind: OrderSequenceStarted, SequenceID: sequenceID})
		changed, err := executeSelectedOrderDocument(ctx, filesystem, order.authorities[index], document.completed)
		if changed {
			changedDocuments++
		}
		event := OrderSequenceEvent{Kind: OrderSequenceDone, SequenceID: sequenceID, Changed: changed}
		if err != nil {
			event.Kind, event.Err = OrderSequenceFailed, err
		}
		events.emit(event)
		if err != nil {
			return changedDocuments, err
		}
	}
	return changedDocuments, nil
}

// Close releases every retained entry authority.
func (order *BoundOrder) Close() error {
	if order == nil || order.closed {
		return nil
	}
	order.closed = true
	err := closeAuthorities(order.authorities)
	order.authorities = nil
	return err
}

func executeSelectedOrderDocument(ctx context.Context, filesystem mutationfs.RootedStore, authority *rootedpath.EntryAuthority, completed relationhost.VerifiedSelectedImage) (bool, error) {
	if err := completed.Validate(); err != nil {
		return false, err
	}
	capability, err := authority.Acquire()
	if err != nil {
		return false, err
	}
	content, mode, identity, err := filesystem.ReadRootedRegularFileUpTo(ctx, capability, completed.Limits().MaximumBytes)
	current := aggregate.ExistingDocument(content)
	if errors.Is(err, os.ErrNotExist) {
		current, err = aggregate.AbsentDocument(), nil
	}
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}
	if current.Equal(completed.Expected()) {
		return false, capability.Close()
	}
	if !current.Equal(completed.Original()) {
		return false, errors.Join(fmt.Errorf("extension order original changed before publication"), capability.Close())
	}
	if !completed.Expected().Exists() || !current.Exists() {
		return false, errors.Join(fmt.Errorf("extension ordering cannot create or remove a document"), capability.Close())
	}
	if err := filesystem.ReplaceRootedFile(ctx, capability, completed.Expected().Content(), mode, identity); err != nil {
		return false, err
	}
	post, err := authority.Acquire()
	if err != nil {
		return true, fmt.Errorf("extension order post-observation: %w", err)
	}
	content, _, _, readErr := filesystem.ReadRootedRegularFileUpTo(ctx, post, completed.Limits().MaximumBytes)
	closeErr := post.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return true, fmt.Errorf("extension order post-observation: %w", err)
	}
	if !aggregate.ExistingDocument(content).Equal(completed.Expected()) {
		return true, fmt.Errorf("extension order post-observation differs from its verified completion")
	}
	return true, nil
}
