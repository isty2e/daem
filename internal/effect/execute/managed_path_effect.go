package execute

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// ManagedPathEffectKind is the closed set of authorized managed-path effects.
type ManagedPathEffectKind string

const (
	ManagedPathEffectCreate  ManagedPathEffectKind = "create"
	ManagedPathEffectReplace ManagedPathEffectKind = "replace"
	ManagedPathEffectRemove  ManagedPathEffectKind = "remove"
	ManagedPathEffectRecord  ManagedPathEffectKind = "record"
)

type managedPathEffectFacts struct {
	subject          topology.SubjectID
	consumerTargets  []target.Target
	scope            target.Scope
	destination      output.Destination
	desiredHash      artifact.ContentHash
	liveHash         artifact.ContentHash
	contentKind      realization.PathProjectionContentKind
	permissionPolicy realization.PathPermissionPolicy
	desiredFileMode  os.FileMode
	liveFileMode     os.FileMode
	previous         *durable.ManagedPathState
}

type (
	managedPathCreateEffect  struct{ facts managedPathEffectFacts }
	managedPathReplaceEffect struct{ facts managedPathEffectFacts }
	managedPathRemoveEffect  struct{ facts managedPathEffectFacts }
	managedPathRecordEffect  struct{ facts managedPathEffectFacts }
)

// ManagedPathEffect is an execution-owned closed union. It contains only
// authority already established by a mutating managed-path decision.
type ManagedPathEffect struct {
	create  *managedPathCreateEffect
	replace *managedPathReplaceEffect
	remove  *managedPathRemoveEffect
	record  *managedPathRecordEffect
}

// ManagedPathEffects admits executable decisions without translating them to
// a wide cross-family action model.
func ManagedPathEffects(decisions []reconcile.ManagedPathDecision) ([]ManagedPathEffect, error) {
	effects := make([]ManagedPathEffect, 0, len(decisions))
	for index, decision := range decisions {
		if decision.IsBlocked() {
			return nil, fmt.Errorf(
				"managed path decision for %q is blocked: %s: %s",
				decision.Destination(),
				decision.Reason(),
				decision.Detail(),
			)
		}
		if decision.IsNoOp() {
			continue
		}
		if decision.PlacementMode() != "" && decision.PlacementMode() != realization.PathProjectionCopy {
			if decision.ContentKind() == realization.PathProjectionDirectory {
				return nil, fmt.Errorf(
					"managed path %s for %q uses unsupported placement mode %q",
					decision.Kind(),
					decision.Destination(),
					decision.PlacementMode(),
				)
			}
			return nil, fmt.Errorf(
				"apply %s mode for %q is not implemented",
				decision.PlacementMode(),
				decision.Destination(),
			)
		}
		facts := managedPathEffectFacts{
			subject:          decision.Subject(),
			consumerTargets:  decision.ConsumerTargets(),
			scope:            decision.Scope(),
			destination:      decision.Destination(),
			desiredHash:      decision.DesiredHash(),
			liveHash:         decision.LiveHash(),
			contentKind:      decision.ContentKind(),
			permissionPolicy: decision.PermissionPolicy(),
			desiredFileMode:  decision.DesiredFileMode(),
			liveFileMode:     decision.LiveFileMode(),
		}
		if previous, ok := decision.PreviousState(); ok {
			copy := previous
			facts.previous = &copy
		}
		var effect ManagedPathEffect
		switch decision.Kind() {
		case reconcile.ManagedPathCreate:
			effect.create = &managedPathCreateEffect{facts: facts}
		case reconcile.ManagedPathReplace:
			effect.replace = &managedPathReplaceEffect{facts: facts}
		case reconcile.ManagedPathRemove:
			effect.remove = &managedPathRemoveEffect{facts: facts}
		case reconcile.ManagedPathRecord:
			effect.record = &managedPathRecordEffect{facts: facts}
		default:
			return nil, fmt.Errorf("managed path decision[%d] has unsupported kind %q", index, decision.Kind())
		}
		if err := effect.validate(); err != nil {
			return nil, fmt.Errorf("managed path effect[%d]: %w", index, err)
		}
		effects = append(effects, effect)
	}
	return effects, nil
}

func (effect ManagedPathEffect) validate() error {
	facts := effect.facts()
	if err := facts.subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if facts.subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("managed path effect requires projection subject")
	}
	if _, err := target.ParseScope(string(facts.scope)); err != nil {
		return err
	}
	if err := facts.destination.ValidateScope(facts.scope); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if facts.contentKind != realization.PathProjectionFile && facts.contentKind != realization.PathProjectionDirectory {
		return fmt.Errorf("content kind %q is not implemented", facts.contentKind)
	}
	if facts.contentKind == realization.PathProjectionDirectory {
		if facts.permissionPolicy != realization.PathPermissionsNone ||
			facts.desiredFileMode != 0 || facts.liveFileMode != 0 {
			return fmt.Errorf("managed directory effect must not carry file modes")
		}
	} else {
		if facts.permissionPolicy != realization.PathPermissionsExecutableClass &&
			facts.permissionPolicy != realization.PathPermissionsExact {
			return fmt.Errorf("managed file permission policy %q is unsupported", facts.permissionPolicy)
		}
		if effect.Kind() == ManagedPathEffectCreate || effect.Kind() == ManagedPathEffectReplace {
			switch facts.permissionPolicy {
			case realization.PathPermissionsExecutableClass:
				if facts.desiredFileMode != 0o600 && facts.desiredFileMode != 0o700 {
					return fmt.Errorf("executable-class managed file write requires a canonical publish mode")
				}
			case realization.PathPermissionsExact:
				if facts.desiredFileMode != facts.desiredFileMode.Perm() {
					return fmt.Errorf("exact managed file write mode must contain permission bits only")
				}
			}
		}
	}
	if facts.previous != nil && facts.previous.Subject() != facts.subject {
		return fmt.Errorf("previous managed state subject does not match effect subject")
	}
	if effect.Kind() == ManagedPathEffectRemove {
		if len(facts.consumerTargets) != 0 {
			return fmt.Errorf("remove effect must not retain consumers")
		}
		if facts.previous == nil {
			return fmt.Errorf("remove effect requires previous managed state and fresh live hash")
		}
		if facts.liveHash == "" {
			return fmt.Errorf("remove effect requires previous managed state and fresh live hash")
		}
		if err := facts.liveHash.Validate(); err != nil {
			return fmt.Errorf("remove effect live hash: %w", err)
		}
		if facts.liveHash != facts.previous.ContentHash() {
			return fmt.Errorf("remove effect live hash does not match previous managed state")
		}
		if !facts.previous.PermissionPolicy().AcceptsMode(facts.previous.FileMode(), facts.liveFileMode) {
			return fmt.Errorf("remove effect live mode does not match previous managed state")
		}
		return nil
	}
	if len(facts.consumerTargets) == 0 {
		return fmt.Errorf("effect requires at least one consumer target")
	}
	previous := target.Target("")
	for index, consumer := range facts.consumerTargets {
		if _, err := target.ParseTarget(string(consumer)); err != nil {
			return err
		}
		if index > 0 && consumer <= previous {
			return fmt.Errorf("consumer targets must be sorted and duplicate-free")
		}
		previous = consumer
	}
	if err := facts.desiredHash.Validate(); err != nil {
		return fmt.Errorf("desired hash: %w", err)
	}
	if effect.Kind() == ManagedPathEffectReplace {
		if facts.previous == nil {
			return fmt.Errorf("replace effect requires previous managed state")
		}
		if facts.liveHash != "" {
			if err := facts.liveHash.Validate(); err != nil {
				return fmt.Errorf("replace effect live hash: %w", err)
			}
		} else if facts.previous.Scope() == facts.scope && facts.previous.Destination() == facts.destination {
			return fmt.Errorf("in-place replace effect requires fresh live hash")
		}
	}
	if effect.Kind() == ManagedPathEffectRecord {
		if facts.liveHash == "" {
			return fmt.Errorf("record effect requires fresh live hash")
		}
		if err := facts.liveHash.Validate(); err != nil {
			return fmt.Errorf("record effect live hash: %w", err)
		}
		if facts.liveHash != facts.desiredHash {
			return fmt.Errorf("record effect live hash does not match desired hash")
		}
		if !facts.permissionPolicy.AcceptsMode(facts.desiredFileMode, facts.liveFileMode) {
			return fmt.Errorf("record effect live mode does not satisfy desired permission policy")
		}
	}
	return nil
}

func (effect ManagedPathEffect) facts() managedPathEffectFacts {
	switch {
	case effect.create != nil:
		return effect.create.facts
	case effect.replace != nil:
		return effect.replace.facts
	case effect.remove != nil:
		return effect.remove.facts
	case effect.record != nil:
		return effect.record.facts
	default:
		return managedPathEffectFacts{}
	}
}

func (effect ManagedPathEffect) Kind() ManagedPathEffectKind {
	switch {
	case effect.create != nil:
		return ManagedPathEffectCreate
	case effect.replace != nil:
		return ManagedPathEffectReplace
	case effect.remove != nil:
		return ManagedPathEffectRemove
	case effect.record != nil:
		return ManagedPathEffectRecord
	default:
		return ""
	}
}

func (effect ManagedPathEffect) Subject() topology.SubjectID { return effect.facts().subject }
func (effect ManagedPathEffect) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), effect.facts().consumerTargets...)
}
func (effect ManagedPathEffect) Scope() target.Scope             { return effect.facts().scope }
func (effect ManagedPathEffect) Destination() output.Destination { return effect.facts().destination }

func (effect ManagedPathEffect) DesiredHash() artifact.ContentHash { return effect.facts().desiredHash }

func (effect ManagedPathEffect) LiveHash() artifact.ContentHash { return effect.facts().liveHash }

func (effect ManagedPathEffect) ContentKind() realization.PathProjectionContentKind {
	return effect.facts().contentKind
}

func (effect ManagedPathEffect) PermissionPolicy() realization.PathPermissionPolicy {
	return effect.facts().permissionPolicy
}

func (effect ManagedPathEffect) DesiredFileMode() os.FileMode {
	return effect.facts().desiredFileMode
}

func (effect ManagedPathEffect) LiveFileMode() os.FileMode {
	return effect.facts().liveFileMode
}

// StateFileMode returns the exact permission baseline that durable state must
// retain, or zero when executable class is the complete permission contract.
func (effect ManagedPathEffect) StateFileMode() os.FileMode {
	if effect.PermissionPolicy() != realization.PathPermissionsExact {
		return 0
	}
	if effect.Kind() == ManagedPathEffectRecord {
		return effect.LiveFileMode().Perm()
	}
	return effect.DesiredFileMode().Perm()
}

func (effect ManagedPathEffect) PreviousState() (durable.ManagedPathState, bool) {
	previous := effect.facts().previous
	if previous == nil {
		return durable.ManagedPathState{}, false
	}
	return *previous, true
}

// RequiresPayload reports whether execution must materialize exact Supply
// bytes for this effect.
func (effect ManagedPathEffect) RequiresPayload() bool {
	return effect.Kind() == ManagedPathEffectCreate || effect.Kind() == ManagedPathEffectReplace
}

func managedPathJournalMutations(effects []ManagedPathEffect) ([]journal.ManagedPathMutation, error) {
	mutations := make([]journal.ManagedPathMutation, 0, len(effects)+1)
	for index, effect := range effects {
		previous, hasPrevious := effect.PreviousState()
		var mutation journal.ManagedPathMutation
		var err error
		switch effect.Kind() {
		case ManagedPathEffectCreate:
			mutation, err = journal.NewManagedPathCreateMutation(
				effect.Subject(), effect.ConsumerTargets(), effect.Scope(), effect.Destination(),
				effect.DesiredHash(), effect.ContentKind(), effect.DesiredFileMode(),
				managedPathPreviousPointer(previous, hasPrevious),
			)
		case ManagedPathEffectReplace:
			if !hasPrevious {
				return nil, fmt.Errorf("managed path effect[%d] replace lacks previous state", index)
			}
			if previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination() {
				create, createErr := journal.NewManagedPathCreateMutation(
					effect.Subject(), effect.ConsumerTargets(), effect.Scope(), effect.Destination(),
					effect.DesiredHash(), effect.ContentKind(), effect.DesiredFileMode(), nil,
				)
				if createErr != nil {
					return nil, fmt.Errorf("managed path effect[%d] relocation creation: %w", index, createErr)
				}
				remove, removeErr := journal.NewManagedPathRemoveMutation(previous, previous.ContentHash())
				if removeErr != nil {
					return nil, fmt.Errorf("managed path effect[%d] relocation removal: %w", index, removeErr)
				}
				mutations = append(mutations, create, remove)
				continue
			}
			mutation, err = journal.NewManagedPathReplaceMutation(
				effect.Subject(), effect.ConsumerTargets(), effect.Scope(), effect.Destination(),
				effect.DesiredHash(), effect.LiveHash(), effect.ContentKind(), effect.DesiredFileMode(), previous,
			)
		case ManagedPathEffectRemove:
			if !hasPrevious {
				return nil, fmt.Errorf("managed path effect[%d] remove lacks previous state", index)
			}
			mutation, err = journal.NewManagedPathRemoveMutation(previous, effect.LiveHash())
		case ManagedPathEffectRecord:
			expectedMode := effect.DesiredFileMode()
			if effect.ContentKind() == realization.PathProjectionFile {
				expectedMode = effect.LiveFileMode()
			}
			mutation, err = journal.NewManagedPathRecordMutation(
				effect.Subject(), effect.ConsumerTargets(), effect.Scope(), effect.Destination(),
				effect.DesiredHash(), effect.LiveHash(), effect.ContentKind(), expectedMode,
				managedPathPreviousPointer(previous, hasPrevious),
			)
		default:
			return nil, fmt.Errorf("managed path effect[%d] has invalid kind %q", index, effect.Kind())
		}
		if err != nil {
			return nil, fmt.Errorf("managed path effect[%d] journal mutation: %w", index, err)
		}
		mutations = append(mutations, mutation)
	}
	return mutations, nil
}

func managedPathPreviousPointer(previous durable.ManagedPathState, present bool) *durable.ManagedPathState {
	if !present {
		return nil
	}
	copy := previous
	return &copy
}
