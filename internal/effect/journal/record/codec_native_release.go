package record

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativeReleaseIdentity struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Scope       string `json:"scope"`
	Destination string `json:"destination"`
	Ownership   string `json:"ownership"`
}

type nativeReleaseFile struct {
	Role         string                      `json:"role"`
	Target       recoveryDocumentObservation `json:"target"`
	BeforeExists bool                        `json:"before_exists"`
	BeforeSHA256 string                      `json:"before_sha256"`
	BeforeMode   uint32                      `json:"before_mode"`
	AfterSHA256  string                      `json:"after_sha256"`
	Write        bool                        `json:"write"`
}

type nativeReleaseBinding struct {
	Owner  recoveryStateAuthority       `json:"owner"`
	Before recoveryDocumentBindingPhase `json:"before"`
	After  recoveryDocumentBindingPhase `json:"after"`
}

type nativeReleaseJournalRecord struct {
	Version          int                         `json:"version"`
	Purpose          string                      `json:"purpose"`
	OperationID      string                      `json:"operation_id"`
	Operation        string                      `json:"operation"`
	CreatedAt        string                      `json:"created_at"`
	StateAuthority   recoveryStateAuthority      `json:"state_authority"`
	StatefileBefore  json.RawMessage             `json:"statefile_before"`
	StatefileAfter   json.RawMessage             `json:"statefile_after"`
	Selection        nativeReleaseIdentity       `json:"selection"`
	Namespace        recoveryDocumentObservation `json:"namespace"`
	LocalFiles       []nativeReleaseFile         `json:"local_files"`
	OwnClaimsBefore  []recoveryClaimValue        `json:"own_claims_before"`
	OwnClaimsRemoved []recoveryClaimValue        `json:"own_claims_removed"`
	Binding          nativeReleaseBinding        `json:"binding"`
}

func (identity *nativeReleaseIdentity) UnmarshalJSON(content []byte) error {
	if err := requireDocumentEvidenceFields(content, "name", "target", "scope", "destination", "ownership"); err != nil {
		return err
	}
	type wire nativeReleaseIdentity
	return decodeRecoveryJSONStrict(content, (*wire)(identity))
}

func (file *nativeReleaseFile) UnmarshalJSON(content []byte) error {
	if err := requireDocumentEvidenceFields(content, "role", "target", "before_exists", "before_sha256", "before_mode", "after_sha256", "write"); err != nil {
		return err
	}
	type wire nativeReleaseFile
	return decodeRecoveryJSONStrict(content, (*wire)(file))
}

func (binding *nativeReleaseBinding) UnmarshalJSON(content []byte) error {
	if err := requireDocumentEvidenceFields(content, "owner", "before", "after"); err != nil {
		return err
	}
	type wire nativeReleaseBinding
	return decodeRecoveryJSONStrict(content, (*wire)(binding))
}

func (release NativeRelease) encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native release context is required")
	}
	if err := release.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, after, err := EncodeSnapshots(release.before, release.after, codec)
	if err != nil {
		return nil, err
	}
	claims, err := persistedDocumentClaims(release.metadata.BeforeClaims())
	if err != nil {
		return nil, err
	}
	removed, err := persistedDocumentClaims(release.metadata.RemovedClaims())
	if err != nil {
		return nil, err
	}
	owner := release.owner
	if binding, present := release.metadata.BindingBefore().Get(); present {
		owner = binding.Owner()
	}
	files := make([]nativeReleaseFile, len(release.files))
	for index, file := range release.files {
		files[index] = nativeReleaseFile{
			Target: file.target.value,
			Role:   string(file.Role()), BeforeExists: file.BeforeExists(),
			BeforeSHA256: file.BeforeSHA256(), BeforeMode: uint32(file.BeforeMode()), AfterSHA256: file.AfterSHA256(), Write: file.Writes(),
		}
	}
	management := release.management
	record := nativeReleaseJournalRecord{
		Version: native17Version, Purpose: "release-only", Operation: "unmanage",
		OperationID: release.operationID, CreatedAt: release.createdAt.Format(time.RFC3339),
		StateAuthority: persistedReleaseOwner(release.owner), StatefileBefore: before, StatefileAfter: after,
		Selection: nativeReleaseIdentity{
			Name: management.Declaration().Name(), Target: string(management.Target()),
			Scope: string(management.Scope()), Destination: management.Destination().String(), Ownership: string(management.Ownership()),
		},
		Namespace: release.namespace, LocalFiles: files, OwnClaimsBefore: claims, OwnClaimsRemoved: removed,
		Binding: nativeReleaseBinding{Owner: persistedReleaseOwner(owner), Before: persistedDocumentBindingValue(release.metadata.BindingBefore()), After: persistedDocumentBindingValue(release.metadata.BindingAfter())},
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	if int64(len(content)) > MaximumBytes {
		return nil, fmt.Errorf("native release exceeds the journal byte envelope")
	}
	decoded, err := decodeNativeRelease(ctx, content, codec)
	if err != nil {
		return nil, err
	}
	if !decoded.before.Equal(release.before) || !decoded.after.Equal(release.after) {
		return nil, fmt.Errorf("native release Snapshot codec changed complete state facts")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func persistedReleaseOwner(owner stateauthority.Authority) recoveryStateAuthority {
	return recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(owner.StatefileAuthority()), ManifestPath: owner.ManifestPath()}
}

func decodeNativeRelease(ctx context.Context, content []byte, codec durable.SnapshotCodec) (NativeRelease, error) {
	if ctx == nil || codec == nil {
		return NativeRelease{}, fmt.Errorf("native release requires context and Snapshot codec")
	}
	if err := ctx.Err(); err != nil {
		return NativeRelease{}, err
	}
	if int64(len(content)) > MaximumBytes {
		return NativeRelease{}, fmt.Errorf("native release exceeds the journal byte envelope")
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "native release journal", MaximumJSONDepth)
	if err != nil {
		return NativeRelease{}, err
	}
	if version != native17Version {
		return NativeRelease{}, UnsupportedVersion(version)
	}
	if err := requireRecoveryJSONFields(content, "native17 release", "version", "purpose", "operation_id", "operation", "created_at", "state_authority", "statefile_before", "statefile_after", "selection", "namespace", "local_files", "own_claims_before", "own_claims_removed", "binding"); err != nil {
		return NativeRelease{}, err
	}
	var record nativeReleaseJournalRecord
	if err := decodeRecoveryJSONStrict(content, &record); err != nil {
		return NativeRelease{}, err
	}
	if record.Version != native17Version || record.Purpose != "release-only" || record.Operation != "unmanage" || !IsSafeOperationID(record.OperationID) {
		return NativeRelease{}, fmt.Errorf("native release requires its closed purpose and operation identity")
	}
	createdAt, err := time.Parse(time.RFC3339, record.CreatedAt)
	if err != nil {
		return NativeRelease{}, err
	}
	owner, err := record.StateAuthority.canonical()
	if err != nil {
		return NativeRelease{}, err
	}
	before, err := codec.Decode(record.StatefileBefore)
	if err != nil {
		return NativeRelease{}, err
	}
	after, err := codec.Decode(record.StatefileAfter)
	if err != nil {
		return NativeRelease{}, err
	}
	id, err := entity.New(entity.KindHostConfig, record.Selection.Name)
	if err != nil {
		return NativeRelease{}, err
	}
	identities, err := journaldocument.HostConfigManagements(ctx, before, id)
	if err != nil {
		return NativeRelease{}, err
	}
	var management journaldocument.HostConfigManagement
	found := false
	for _, identity := range identities {
		if string(identity.Target()) == record.Selection.Target && string(identity.Scope()) == record.Selection.Scope &&
			identity.Destination().String() == record.Selection.Destination && string(identity.Ownership()) == record.Selection.Ownership {
			management = identity
			found = true
		}
	}
	if !found {
		return NativeRelease{}, fmt.Errorf("native release selected management identity is absent or contradictory")
	}
	claims, err := releaseClaims(record.OwnClaimsBefore)
	if err != nil {
		return NativeRelease{}, err
	}
	removed, err := releaseClaims(record.OwnClaimsRemoved)
	if err != nil {
		return NativeRelease{}, err
	}
	bindingOwner, err := record.Binding.Owner.canonical()
	if err != nil || !bindingOwner.Equal(owner) {
		return NativeRelease{}, fmt.Errorf("native release binding owner differs from the selected state authority")
	}
	bindingBefore, err := record.Binding.Before.canonicalValue(bindingOwner, management.Destination())
	if err != nil {
		return NativeRelease{}, err
	}
	bindingAfter, err := record.Binding.After.canonicalValue(bindingOwner, management.Destination())
	if err != nil {
		return NativeRelease{}, err
	}
	files := make([]NativeReleaseFileIntent, len(record.LocalFiles))
	for index, file := range record.LocalFiles {
		files[index], err = NewNativeReleaseFileIntent(NativeReleaseFileInput{
			Target: NativeReleaseNamespace{value: file.Target},
			Role:   NativeReleaseFileRole(file.Role), BeforeExists: file.BeforeExists,
			BeforeSHA256: file.BeforeSHA256, BeforeMode: os.FileMode(file.BeforeMode), AfterSHA256: file.AfterSHA256, Write: file.Write,
		})
		if err != nil {
			return NativeRelease{}, err
		}
	}
	release, err := newNativeRelease(ctx, record.OperationID, createdAt, owner, before, management, record.Namespace, claims, bindingBefore, files, codec)
	if err != nil {
		return NativeRelease{}, err
	}
	if !release.after.Equal(after) || !sameReleaseClaims(release.metadata.RemovedClaims(), removed) || !release.metadata.BindingAfter().Equal(bindingAfter) {
		return NativeRelease{}, fmt.Errorf("native release after facts differ from its complete selected management delta")
	}
	return release, nil
}

func releaseClaims(records []recoveryClaimValue) ([]ownership.Claim, error) {
	claims := make([]ownership.Claim, 0, len(records))
	for _, record := range records {
		value, err := canonicalClaimValue(record)
		if err != nil {
			return nil, err
		}
		claim, present := value.Get()
		if !present {
			return nil, fmt.Errorf("native release claim arrays cannot contain absent rows")
		}
		claims = append(claims, claim)
	}
	registry, err := ownership.NewRegistry(claims)
	if err != nil {
		return nil, err
	}
	return registry.Claims(), nil
}

func sameReleaseClaims(left, right []ownership.Claim) bool {
	if len(left) != len(right) {
		return false
	}
	for index, claim := range left {
		if !claim.Equal(right[index]) {
			return false
		}
	}
	return true
}
