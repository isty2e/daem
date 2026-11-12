package fileset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// PublicationMaterializer derives one After payload from its verified Before
// bytes and caller-captured immutable facts. It must not read live participants.
type PublicationMaterializer func(context.Context, []byte) ([]byte, error)

// PublicationInput supplies an independently qualified selection. Callers hold
// the complete target/evidence leases through publication and compensation;
// decoding a persisted intent does not establish that qualification.
type PublicationInput struct {
	EvidenceRoot       string
	Intent             PublicationIntent
	Materializers      map[string]PublicationMaterializer
	PhysicalWorkBudget PhysicalWorkBudget
}

// CommitFreshFileSet5 refuses pending evidence, admits complete bounded work,
// then publishes lazy After images under a containing intent. True no-ops do
// not inspect or create the evidence root. No runtime format is selected here.
func CommitFreshFileSet5(ctx context.Context, input PublicationInput) error {
	if ctx == nil {
		return fmt.Errorf("publication context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.Intent.Validate(); err != nil {
		return err
	}
	materializers := make(map[string]PublicationMaterializer, len(input.Materializers))
	for path, materialize := range input.Materializers {
		materializers[path] = materialize
	}
	changed := 0
	for _, target := range input.Intent.targets {
		materialize, present := materializers[target.path]
		if target.Writes() != present || present && materialize == nil {
			return fmt.Errorf("publication materializers must match changed namespaces exactly")
		}
		if target.Writes() {
			changed++
		}
	}
	if len(materializers) != changed {
		return fmt.Errorf("publication materializer names an unselected namespace")
	}
	if !input.Intent.HasChanges() {
		return nil
	}
	body, header, err := encodePublicationEvidence(input.Intent)
	if err != nil {
		return err
	}
	plan, err := bindPublicationPlan(ctx, input.EvidenceRoot, input.Intent, int64(len(header)), int64(len(body)), input.PhysicalWorkBudget)
	if err != nil {
		return err
	}
	defer plan.rootWitness.Close()
	if err := ObserveClearPublicationFenceAt(ctx, plan.root, mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget); err != nil {
		return err
	}
	if err := plan.admitWork(input.PhysicalWorkBudget, publicationForwardWork); err != nil {
		return err
	}
	if err := plan.publishEvidence(ctx, body, header); err != nil {
		return err
	}
	for index, target := range plan.intent.targets {
		if !target.Writes() {
			continue
		}
		if err := plan.publishTarget(ctx, index, materializers[target.path]); err != nil {
			if _, settlementErr := plan.settle(context.WithoutCancel(ctx), hashBytes(header)); settlementErr != nil {
				return fmt.Errorf("%w; publication settlement refused: %v", err, settlementErr)
			}
			return err
		}
	}
	disposition, err := plan.settle(context.WithoutCancel(ctx), hashBytes(header))
	if err != nil {
		return err
	}
	if disposition != RecoveryFinalize {
		return fmt.Errorf("publication restored Before instead of reaching the complete After set")
	}
	return nil
}

func (plan *publicationPlan) publishEvidence(ctx context.Context, body, header []byte) error {
	capability, err := plan.rootWitness.Acquire(plan.destination)
	if err != nil {
		return err
	}
	prepared, err := storagecommit.PrepareRootedTreeWithLimits(ctx, capability, plan.limits, func(writer mutationfs.RootedTreeWriter) error {
		for index, target := range plan.intent.targets {
			image, content, _, err := readPublicationTarget(ctx, target.path, target.before.size)
			if err != nil {
				return err
			}
			if !image.Equal(target.before) {
				return fmt.Errorf("%w: Before image changed for %q", ErrRecoveryIntentMismatch, target.path)
			}
			if target.before.exists {
				if err := writePublicationEvidenceFile(writer, publicationBackupName(index), content); err != nil {
					return err
				}
			}
		}
		if err := writePublicationEvidenceFile(writer, publicationIntentFile, body); err != nil {
			return err
		}
		return writePublicationEvidenceFile(writer, transactionMarkerFile, header)
	})
	if err != nil {
		return err
	}
	defer prepared.Abort(context.WithoutCancel(ctx))
	return prepared.Commit(ctx)
}

func writePublicationEvidenceFile(writer mutationfs.RootedTreeWriter, name string, content []byte) error {
	path, err := mutationfs.NewTreeRelativePath(name)
	if err != nil {
		return err
	}
	return writer.WriteFile(path, transactionEvidenceMode, bytes.NewReader(content))
}

func (plan *publicationPlan) publishTarget(ctx context.Context, index int, materialize PublicationMaterializer) error {
	target := plan.intent.targets[index]
	current, _, identity, err := readPublicationTarget(ctx, target.path, target.before.size)
	if err != nil {
		return err
	}
	if !current.Equal(target.before) {
		return fmt.Errorf("publication target %q changed before its write", target.path)
	}
	before, err := plan.readBackup(ctx, index)
	if err != nil {
		return err
	}
	after, err := materialize(ctx, before)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return err
	}
	image, err := NewPublicationImage(after, target.after.mode)
	if err != nil {
		return err
	}
	if !image.Equal(target.after) {
		return fmt.Errorf("materialized After image differs from the intent for %q", target.path)
	}
	return commitPublicationImage(ctx, target.path, current, identity, after, target.after.mode)
}

func readPublicationTarget(ctx context.Context, path string, maximumBytes int64) (PublicationImage, []byte, storagecommit.EntryIdentity, error) {
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, maximumBytes)
	if canceled := ctx.Err(); canceled != nil {
		return PublicationImage{}, nil, storagecommit.EntryIdentity{}, canceled
	}
	if errors.Is(err, os.ErrNotExist) {
		return AbsentPublicationImage(), nil, storagecommit.EntryIdentity{}, nil
	}
	if err != nil {
		return PublicationImage{}, nil, storagecommit.EntryIdentity{}, err
	}
	content := snapshot.Content()
	image, err := NewPublicationImage(content, snapshot.Mode())
	if err != nil {
		return PublicationImage{}, nil, storagecommit.EntryIdentity{}, err
	}
	identity, ok := snapshot.Identity().(storagecommit.EntryIdentity)
	if !ok {
		return PublicationImage{}, nil, storagecommit.EntryIdentity{}, fmt.Errorf("storage snapshot has no commit identity")
	}
	return image, content, identity, nil
}

func commitPublicationImage(ctx context.Context, path string, current PublicationImage, identity storagecommit.EntryIdentity, content []byte, mode fs.FileMode) error {
	var request storagecommit.FileCommit
	var err error
	if current.exists {
		request, err = storagecommit.NewFileReplacement(path, content, mode, identity)
	} else {
		request, err = storagecommit.NewFileCreate(path, content, mode)
	}
	if err != nil {
		return err
	}
	return storagecommit.CommitFile(ctx, request)
}

func (plan *publicationPlan) readBackup(ctx context.Context, index int) ([]byte, error) {
	before := plan.intent.targets[index].before
	if !before.exists {
		return nil, nil
	}
	content, _, err := readEvidenceImageWithBudget(ctx,
		filepath.Join(transactionDir(plan.root), publicationBackupName(index)), "publication Before image",
		0, before.hash, before.size, mutationfs.MaximumPhysicalPathDepth, nil)
	return content, err
}
