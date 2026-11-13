package record

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/topology"
)

type NativeReleaseInput struct {
	OperationID     string
	CreatedAt       time.Time
	Owner           stateauthority.Authority
	Before          durable.Snapshot
	Management      journaldocument.HostConfigManagement
	Namespace       NativeReleaseNamespace
	OwnClaimsBefore []ownership.Claim
	BindingBefore   ownership.DocumentBindingValue
	Files           []NativeReleaseFileIntent
}

// NativeReleaseNamespace retains comparison facts, not a current filesystem
// observation. It cannot supply a host writer or turn a key into an I/O locator.
type NativeReleaseNamespace struct{ value recoveryDocumentObservation }

func NewNativeReleaseExactNamespace(exact pathauthority.Exact) (NativeReleaseNamespace, error) {
	if err := exact.Validate(); err != nil {
		return NativeReleaseNamespace{}, err
	}
	return NativeReleaseNamespace{value: recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(exact)}}, nil
}

func NewNativeReleaseProvisionalNamespace(missing pathauthority.Provisional) (NativeReleaseNamespace, error) {
	if err := missing.Validate(); err != nil {
		return NativeReleaseNamespace{}, err
	}
	return NativeReleaseNamespace{value: recoveryDocumentObservation{
		Kind:      "provisional",
		Authority: &pathAuthorityDTO{Key: missing.CandidateKey(), Witness: missing.CandidateWitness()}, NamespaceAuthority: persistedPathAuthority(missing.Namespace()),
	}}, nil
}

func (namespace NativeReleaseNamespace) matches(other NativeReleaseNamespace) bool {
	if namespace.value.validate() != nil || other.value.validate() != nil {
		return false
	}
	if namespace.value.Kind == "exact" {
		exact, _ := canonicalPathAuthority(*namespace.value.Authority)
		return other.value.matchesRetainedExact(exact)
	}
	if other.value.Kind == "exact" {
		exact, _ := canonicalPathAuthority(*other.value.Authority)
		return namespace.value.matchesRetainedExact(exact)
	}
	return namespace.value.equal(other.value)
}

// NativeRelease is the closed metadata-only native17 purpose. Its scoped facts
// grant neither a host-effect program nor filesystem access to recorded keys.
type NativeRelease struct {
	operationID string
	createdAt   time.Time
	owner       stateauthority.Authority
	before      durable.Snapshot
	after       durable.Snapshot
	management  journaldocument.HostConfigManagement
	namespace   recoveryDocumentObservation
	metadata    ownership.OwnDocumentRelease
	files       []NativeReleaseFileIntent
	initialized bool
}

func NewNativeRelease(ctx context.Context, input NativeReleaseInput, codec durable.SnapshotCodec) (NativeRelease, error) {
	namespace := input.Namespace.value
	if err := namespace.validate(); err != nil {
		return NativeRelease{}, err
	}
	return newNativeRelease(ctx, input.OperationID, input.CreatedAt, input.Owner, input.Before,
		input.Management, namespace, input.OwnClaimsBefore, input.BindingBefore, input.Files, codec)
}

func newNativeRelease(ctx context.Context, operationID string, createdAt time.Time, owner stateauthority.Authority,
	before durable.Snapshot, management journaldocument.HostConfigManagement, namespace recoveryDocumentObservation,
	claims []ownership.Claim, binding ownership.DocumentBindingValue, files []NativeReleaseFileIntent,
	codec durable.SnapshotCodec,
) (NativeRelease, error) {
	if ctx == nil || codec == nil {
		return NativeRelease{}, fmt.Errorf("native release requires context and Snapshot codec")
	}
	if err := ctx.Err(); err != nil {
		return NativeRelease{}, err
	}
	if !IsSafeOperationID(operationID) {
		return NativeRelease{}, fmt.Errorf("native release requires a safe operation identity")
	}
	if _, err := time.Parse(time.RFC3339, createdAt.UTC().Format(time.RFC3339)); err != nil {
		return NativeRelease{}, fmt.Errorf("native release creation time: %w", err)
	}
	if err := owner.Validate(); err != nil {
		return NativeRelease{}, err
	}
	if err := namespace.validate(); err != nil {
		return NativeRelease{}, err
	}
	after, err := management.RemoveFrom(ctx, before)
	if err != nil {
		return NativeRelease{}, err
	}
	if _, _, err := EncodeSnapshots(before, after, codec); err != nil {
		return NativeRelease{}, err
	}
	canonicalFiles, err := canonicalReleaseFiles(files)
	if err != nil {
		return NativeRelease{}, err
	}
	if !canonicalFiles[2].target.value.matchesRetainedExact(owner.StatefileAuthority()) {
		return NativeRelease{}, fmt.Errorf("native release state target differs from its owner authority")
	}
	scope, err := releaseDocumentScope(namespace)
	if err != nil {
		return NativeRelease{}, err
	}
	selected := make(map[topology.SubjectID]struct{})
	for _, subject := range management.Subjects() {
		selected[subject] = struct{}{}
	}
	footprints := make(map[output.Footprint]struct{})
	for _, row := range before.ManagedPaths() {
		if _, present := selected[row.Subject()]; present {
			footprints[output.WholePathFootprint()] = struct{}{}
		}
	}
	for _, row := range before.ManagedAggregates() {
		if _, present := selected[row.Subject()]; present {
			footprints[row.Contribution().Footprint()] = struct{}{}
		}
	}
	var removed []ownership.ManagedAddress
	for _, claim := range claims {
		if err := ctx.Err(); err != nil {
			return NativeRelease{}, err
		}
		if _, selected := footprints[claim.Address().Footprint()]; selected {
			removed = append(removed, claim.Address())
		}
	}
	index, err := journaldocument.IndexSnapshotForClaims(ctx, after, journaldocument.NativeClaims)
	if err != nil {
		return NativeRelease{}, err
	}
	destinations, err := index.NativeDestinations(ctx)
	if err != nil {
		return NativeRelease{}, err
	}
	_, retainBinding := destinations[management.Destination()]
	metadata, err := ownership.NewOwnDocumentRelease(ownership.OwnDocumentReleaseInput{
		Owner: owner, Scope: scope, BeforeClaims: claims, RemovalAddresses: removed,
		BindingDestination: management.Destination(), BindingBefore: binding, RetainBinding: retainBinding,
	})
	if err != nil {
		return NativeRelease{}, err
	}
	return NativeRelease{
		operationID: operationID, createdAt: createdAt.UTC(), owner: owner,
		before: before, after: after, management: management, namespace: namespace,
		metadata: metadata, files: canonicalFiles, initialized: true,
	}, nil
}

func releaseDocumentScope(namespace recoveryDocumentObservation) (ownership.DocumentScope, error) {
	if err := namespace.validate(); err != nil {
		return ownership.DocumentScope{}, err
	}
	if namespace.Kind == "exact" {
		exact, err := canonicalPathAuthority(*namespace.Authority)
		if err != nil {
			return ownership.DocumentScope{}, err
		}
		return ownership.NewDocumentScope([]pathauthority.Exact{exact}, nil)
	}
	missing, err := namespace.provisional()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	return ownership.NewDocumentScope(nil, []pathauthority.Provisional{missing})
}

func (release NativeRelease) Validate() error {
	if !release.initialized {
		return fmt.Errorf("native metadata release is required")
	}
	return nil
}

func (release NativeRelease) OperationID() string             { return release.operationID }
func (release NativeRelease) Owner() stateauthority.Authority { return release.owner }
func (release NativeRelease) Before() durable.Snapshot        { return release.before }
func (release NativeRelease) After() durable.Snapshot         { return release.after }
func (release NativeRelease) Management() journaldocument.HostConfigManagement {
	return release.management
}
func (release NativeRelease) Metadata() ownership.OwnDocumentRelease { return release.metadata }
func (release NativeRelease) LocalFiles() []NativeReleaseFileIntent {
	return slices.Clone(release.files)
}

func (release NativeRelease) Encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	return release.encode(ctx, codec)
}

// MatchLocalTargets compares independently selected control paths. It grants no
// access to the recorded reference and is required even without a FileSet marker.
func (release NativeRelease) MatchLocalTargets(manifest, defaultLock, state NativeReleaseNamespace) error {
	if err := release.Validate(); err != nil {
		return err
	}
	for index, current := range []NativeReleaseNamespace{manifest, defaultLock, state} {
		if !release.files[index].target.matches(current) {
			return fmt.Errorf("native release %s target differs from the independently selected path", release.files[index].role)
		}
	}
	return nil
}

func (release NativeRelease) DocumentScope() (ownership.DocumentScope, error) {
	if err := release.Validate(); err != nil {
		return ownership.DocumentScope{}, err
	}
	return releaseDocumentScope(release.namespace)
}
