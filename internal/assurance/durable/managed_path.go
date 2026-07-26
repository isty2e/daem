package durable

import (
	"fmt"
	"os"
	"slices"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ManagedPathState is the durable baseline for one subject-owned path.
type ManagedPathState struct {
	subject          topology.SubjectID
	consumerTargets  []target.Target
	scope            target.Scope
	destination      output.Destination
	contentHash      artifact.ContentHash
	contentKind      realization.PathProjectionContentKind
	permissionPolicy realization.PathPermissionPolicy
	fileMode         os.FileMode
}

// NewManagedPathState constructs one validated managed-path baseline.
func NewManagedPathState(
	subject topology.SubjectID,
	consumerTargets []target.Target,
	scope target.Scope,
	destination output.Destination,
	contentHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
	fileMode os.FileMode,
) (ManagedPathState, error) {
	canonicalTargets, err := target.CanonicalSet(consumerTargets)
	if err != nil {
		return ManagedPathState{}, fmt.Errorf("managed path state consumer targets: %w", err)
	}
	state := ManagedPathState{
		subject:          subject,
		consumerTargets:  canonicalTargets,
		scope:            scope,
		destination:      destination,
		contentHash:      contentHash,
		contentKind:      contentKind,
		permissionPolicy: permissionPolicy,
		fileMode:         fileMode.Perm(),
	}
	if err := state.validate(); err != nil {
		return ManagedPathState{}, err
	}
	return state, nil
}

func (state ManagedPathState) validate() error {
	if err := state.subject.Validate(); err != nil {
		return fmt.Errorf("managed path state subject: %w", err)
	}
	if state.subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("managed path state requires projection subject")
	}
	_, entityBacked := topologyprojection.EntityID(state.subject)
	if !entityBacked {
		return fmt.Errorf("managed path state requires entity-backed projection subject identity")
	}
	if len(state.consumerTargets) == 0 {
		return fmt.Errorf("managed path state requires at least one consumer target")
	}
	canonicalTargets, err := target.CanonicalSet(state.consumerTargets)
	if err != nil {
		return fmt.Errorf("managed path state consumer targets: %w", err)
	}
	if !slices.Equal(state.consumerTargets, canonicalTargets) {
		return fmt.Errorf("managed path state consumer targets are not canonical")
	}
	if _, err := target.ParseScope(string(state.scope)); err != nil {
		return fmt.Errorf("managed path state scope: %w", err)
	}
	if err := state.destination.ValidateScope(state.scope); err != nil {
		return fmt.Errorf("managed path state destination: %w", err)
	}
	if err := state.contentHash.Validate(); err != nil {
		return fmt.Errorf("managed path state content hash: %w", err)
	}
	if err := realization.ValidateManagedPathPermissionState(
		state.contentKind,
		state.permissionPolicy,
		state.fileMode,
	); err != nil {
		return fmt.Errorf("managed path state: %w", err)
	}
	return nil
}

// Subject returns the projection subject that owns this baseline.
func (state ManagedPathState) Subject() topology.SubjectID { return state.subject }

// ConsumerTargets returns a defensive copy of every target sharing this path.
func (state ManagedPathState) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), state.consumerTargets...)
}

// Scope returns the host scope of the managed path.
func (state ManagedPathState) Scope() target.Scope { return state.scope }

// Destination returns the managed host path.
func (state ManagedPathState) Destination() output.Destination { return state.destination }

// ContentHash returns the last applied content hash.
func (state ManagedPathState) ContentHash() artifact.ContentHash { return state.contentHash }

// ContentKind returns whether this baseline owns a file or directory path.
func (state ManagedPathState) ContentKind() realization.PathProjectionContentKind {
	return state.contentKind
}

// PermissionPolicy returns the persisted permission policy.
func (state ManagedPathState) PermissionPolicy() realization.PathPermissionPolicy {
	return state.permissionPolicy
}

// FileMode returns the exact permission baseline when exact permissions apply.
func (state ManagedPathState) FileMode() os.FileMode { return state.fileMode }

// Equal reports semantic equality between two managed-path baselines.
func (state ManagedPathState) Equal(other ManagedPathState) bool {
	if state.subject != other.subject ||
		state.scope != other.scope ||
		state.destination != other.destination ||
		state.contentHash != other.contentHash ||
		state.contentKind != other.contentKind ||
		state.permissionPolicy != other.permissionPolicy ||
		state.fileMode != other.fileMode ||
		len(state.consumerTargets) != len(other.consumerTargets) {
		return false
	}
	for index := range state.consumerTargets {
		if state.consumerTargets[index] != other.consumerTargets[index] {
			return false
		}
	}
	return true
}
