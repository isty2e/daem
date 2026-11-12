package fileset

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// PublicationIntentEvidenceView is a verified, immutable containing-record
// snapshot. Its marker revision binds the exact bytes, not effect authority.
type PublicationIntentEvidenceView struct {
	evidenceRoot   string
	markerRevision string
	intent         PublicationIntent
	headerSize     int64
	intentSize     int64
}

func (view PublicationIntentEvidenceView) MarkerRevision() string    { return view.markerRevision }
func (view PublicationIntentEvidenceView) Intent() PublicationIntent { return view.intent }

// PublicationBeforeEvidenceView contains only one caller-selected namespace's
// verified Before image. It is a snapshot, not authority over the live target.
type PublicationBeforeEvidenceView struct {
	markerRevision string
	namespace      string
	image          PublicationImage
	content        []byte
}

func (view PublicationBeforeEvidenceView) MarkerRevision() string  { return view.markerRevision }
func (view PublicationBeforeEvidenceView) Namespace() string       { return view.namespace }
func (view PublicationBeforeEvidenceView) Image() PublicationImage { return view.image }

// Content returns a copy; Image().Exists(), not slice nilness, distinguishes
// absence from a present empty file.
func (view PublicationBeforeEvidenceView) Content() []byte { return slices.Clone(view.content) }

// ReadPublicationBeforeEvidence5 selects an exact namespace from the same raw
// marker revision the caller inspected. It opens only the containing evidence
// and selected backup, never the recorded target path or its ancestors.
func ReadPublicationBeforeEvidence5(
	ctx context.Context,
	evidenceRoot string,
	namespace string,
	markerRevision string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (PublicationBeforeEvidenceView, error) {
	if ctx == nil {
		return PublicationBeforeEvidenceView{}, fmt.Errorf("publication Before evidence context is required")
	}
	if err := ctx.Err(); err != nil {
		return PublicationBeforeEvidenceView{}, err
	}
	if !isPublicationHash(markerRevision) {
		return PublicationBeforeEvidenceView{}, fmt.Errorf("%w: expected marker revision is required", ErrRecoveryIntentMismatch)
	}
	view, err := ReadPublicationIntentEvidence5(ctx, evidenceRoot, maximumPhysicalDepth, physicalWorkBudget)
	if err != nil {
		return PublicationBeforeEvidenceView{}, err
	}
	if view.markerRevision != markerRevision {
		return PublicationBeforeEvidenceView{}, fmt.Errorf("%w: marker revision changed", ErrRecoveryIntentMismatch)
	}
	index, found := slices.BinarySearchFunc(view.intent.targets, namespace, func(target PublicationTarget, path string) int {
		return strings.Compare(target.path, path)
	})
	if !found {
		return PublicationBeforeEvidenceView{}, fmt.Errorf("%w: selected namespace is not a participant", ErrRecoveryIntentMismatch)
	}
	image := view.intent.targets[index].before
	var content []byte
	if image.exists {
		content, err = readPublicationEvidenceImage(
			ctx,
			filepath.Join(transactionDir(view.evidenceRoot), publicationBackupName(index)),
			"publication Before image", image.hash, image.size,
			maximumPhysicalDepth, physicalWorkBudget,
		)
		if canceled := ctx.Err(); canceled != nil {
			return PublicationBeforeEvidenceView{}, canceled
		}
		if err != nil {
			return PublicationBeforeEvidenceView{}, wrapFileSetEvidenceInvalid(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return PublicationBeforeEvidenceView{}, err
	}
	return PublicationBeforeEvidenceView{markerRevision: markerRevision, namespace: namespace, image: image, content: content}, nil
}

func publicationBackupName(index int) string {
	return fmt.Sprintf("target-%03d.before", index)
}

// ReadPublicationIntentEvidence5 reads only the caller-selected evidence root.
// Recorded target paths and semantic selection remain untrusted for effects.
func ReadPublicationIntentEvidence5(
	ctx context.Context,
	evidenceRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (PublicationIntentEvidenceView, error) {
	if ctx == nil {
		return PublicationIntentEvidenceView{}, fmt.Errorf("publication evidence context is required")
	}
	if err := ctx.Err(); err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	if maximumPhysicalDepth <= 0 || physicalWorkBudget == nil {
		return PublicationIntentEvidenceView{}, fmt.Errorf("publication evidence requires a path depth and shared work budget")
	}
	canonicalRoot, err := canonicalStateDirBounded(evidenceRoot, maximumPhysicalDepth, physicalWorkBudget)
	if err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	view, err := readPublicationIntentEvidence(ctx, canonicalRoot, maximumPhysicalDepth, physicalWorkBudget)
	if canceled := ctx.Err(); canceled != nil {
		return PublicationIntentEvidenceView{}, canceled
	}
	if err != nil {
		return PublicationIntentEvidenceView{}, wrapFileSetEvidenceInvalid(err)
	}
	return view, nil
}

func readPublicationIntentEvidence(
	ctx context.Context,
	canonicalRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (PublicationIntentEvidenceView, error) {
	path := markerPath(canonicalRoot)
	if physicalWorkBudget != nil {
		depth, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
		if err != nil {
			return PublicationIntentEvidenceView{}, wrapFileSetAccessUnprovable(err)
		}
		work, err := storagecommit.MaximumFileSnapshotWork(depth, maximumMarkerBytes)
		if err != nil {
			return PublicationIntentEvidenceView{}, wrapFileSetAccessUnprovable(err)
		}
		if err := admitFileSetFenceWork(physicalWorkBudget, fileSetPhysicalWork{
			pathComponents: work.PathWork(), entries: work.EntryWork(), bytes: work.ByteWork(),
		}); err != nil {
			return PublicationIntentEvidenceView{}, err
		}
	}
	header, err := readMarkerContent(ctx, path)
	if err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	marker, err := decodePublicationMarker(ctx, header)
	if err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	body, err := readPublicationEvidenceImage(
		ctx,
		filepath.Join(transactionDir(canonicalRoot), publicationIntentFile),
		"publication intent",
		marker.intentHash,
		marker.intentSize,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	intent, err := marker.decodeIntent(ctx, body)
	if err != nil {
		return PublicationIntentEvidenceView{}, err
	}
	return PublicationIntentEvidenceView{
		evidenceRoot: canonicalRoot, markerRevision: hashBytes(header), intent: intent,
		headerSize: int64(len(header)), intentSize: marker.intentSize,
	}, nil
}

func readPublicationEvidenceImage(
	ctx context.Context,
	path, name, expectedHash string,
	expectedSize int64,
	maximumPhysicalDepth int,
	budget PhysicalWorkBudget,
) ([]byte, error) {
	if budget != nil {
		depth, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
		if err != nil {
			return nil, wrapFileSetAccessUnprovable(err)
		}
		work, err := publicationEvidenceImageReadWork(depth, expectedSize)
		if err != nil {
			return nil, wrapFileSetAccessUnprovable(err)
		}
		if err := admitFileSetFenceWork(budget, work); err != nil {
			return nil, err
		}
	}
	// Gen5 has admitted the complete read projection; retain the shared integrity
	// checks without charging its older static census convention a second time.
	content, _, err := readEvidenceImageWithBudget(ctx, path, name, 0, expectedHash, expectedSize, maximumPhysicalDepth, nil)
	return content, err
}

func encodePublicationEvidence(intent PublicationIntent) (body, header []byte, err error) {
	body, err = encodePublicationIntent(intent)
	if err != nil {
		return nil, nil, err
	}
	header, err = encodePublicationMarker(body)
	if err != nil {
		return nil, nil, err
	}
	return body, header, nil
}
