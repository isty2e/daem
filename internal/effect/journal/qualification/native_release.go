package qualification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/fileset"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output/ownership"
)

type NativeReleaseReadOptions struct {
	Paths              journalrecord.NativeReleaseControlPaths
	OperationAuthority *rootedpath.EntryAuthority
	LocalAuthorities   []*rootedpath.EntryAuthority
	Filesystem         mutationfs.RootedReader
	Budget             *physicalwork.Budget
	State              durable.SnapshotReader
	Registry           func(context.Context) (ownership.Registry, error)
}

// NativeReleaseAssessment is produced only after the local FileSet fence is
// clear. Before preserves own metadata; After may still need own convergence.
type NativeReleaseAssessment struct {
	source       journalinventory.ActiveJournal
	after        bool
	needsRelease bool
	valid        bool
}

func (assessment NativeReleaseAssessment) CleanBefore() bool {
	return assessment.valid && !assessment.after
}

func (assessment NativeReleaseAssessment) CleanAfter() bool {
	return assessment.valid && assessment.after && !assessment.needsRelease
}

func (assessment NativeReleaseAssessment) NeedsOwnRelease() bool {
	return assessment.valid && assessment.after && assessment.needsRelease
}

func (assessment NativeReleaseAssessment) MatchesRevision(identity retirement.Identity, directory string, physical journalinventory.ActiveJournalAuthority) bool {
	return assessment.valid && assessment.source.MatchesRevision(identity, directory, physical)
}

func AssessNativeRelease(ctx context.Context, source journalinventory.ActiveJournal, options NativeReleaseReadOptions) (result NativeReleaseAssessment, resultErr error) {
	if ctx == nil || options.OperationAuthority == nil || options.Filesystem == nil || options.Budget == nil || options.State == nil || options.Registry == nil || len(options.LocalAuthorities) != 3 {
		return NativeReleaseAssessment{}, fmt.Errorf("native release assessment requires complete selected control and journal authority")
	}
	release, present := source.Record().NativeRelease()
	if !present {
		return NativeReleaseAssessment{}, fmt.Errorf("native release assessment requires its inventory purpose")
	}
	if err := fileset.ObserveClearFenceAt(ctx, filepath.Dir(options.Paths.State), physicalwork.MaximumPhysicalPathDepth, options.Budget); err != nil {
		return NativeReleaseAssessment{}, err
	}
	if _, err := MatchNativeReleaseTargets(ctx, release, options.Paths, options.Budget); err != nil {
		return NativeReleaseAssessment{}, err
	}
	if err := ValidateNativeRevision(ctx, source.OperationDir(), source.Identity().JournalAuthorityFingerprint(), source.PhysicalAuthority(), options.Filesystem, options.OperationAuthority, options.Budget); err != nil {
		return NativeReleaseAssessment{}, err
	}
	capability, err := options.OperationAuthority.Acquire()
	if err != nil {
		return NativeReleaseAssessment{}, err
	}
	snapshot, readErr := options.Filesystem.SnapshotRootedDirectoryEntries(ctx, capability, 1)
	closeErr := capability.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return NativeReleaseAssessment{}, err
	}
	entries := snapshot.Entries()
	if !source.PhysicalAuthority().Matches(snapshot.RootIdentity()) || snapshot.RootMode() != retirement.DirectoryMode || !snapshot.RootOwnedByInvoker() ||
		len(entries) != 1 || entries[0].Name() != journalrecord.FileName || entries[0].Kind() != mutationfs.EntryKindFile || entries[0].Mode() != journalrecord.Mode || !entries[0].OwnedByInvoker() {
		return NativeReleaseAssessment{}, fmt.Errorf("native release journal-only evidence changed")
	}
	current, err := options.State(ctx)
	if err != nil {
		return NativeReleaseAssessment{}, err
	}
	after := current.Equal(release.After())
	if !after && !current.Equal(release.Before()) {
		return NativeReleaseAssessment{}, fmt.Errorf("native release current state matches neither complete Before nor After")
	}
	files := release.LocalFiles()
	paths := []string{options.Paths.Manifest, options.Paths.DefaultLock, options.Paths.State}
	for index, file := range files {
		if options.LocalAuthorities[index] == nil {
			return NativeReleaseAssessment{}, fmt.Errorf("native release local target authority is missing")
		}
		capability, err := options.LocalAuthorities[index].Acquire()
		if err != nil {
			return NativeReleaseAssessment{}, err
		}
		if err := RequireOperationDestination(capability, paths[index]); err != nil {
			return NativeReleaseAssessment{}, errors.Join(err, capability.Close())
		}
		if file.Role() != journalrecord.NativeReleaseState {
			if err := capability.Close(); err != nil {
				return NativeReleaseAssessment{}, err
			}
			continue
		}

		maximum := min(journalrecord.MaximumBytes, options.Budget.RemainingBytes())
		if maximum <= 0 {
			return NativeReleaseAssessment{}, errors.Join(fmt.Errorf("native release control observation exceeds its byte allocation"), capability.Close())
		}
		content, mode, _, readErr := options.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
		closeErr := capability.Close()
		charged := int64(len(content))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			charged = maximum
		}
		work, workErr := workmeasure.NewArtifactWork(1, charged)
		if workErr == nil {
			workErr = options.Budget.AdmitTree(work)
		}
		if workErr != nil {
			return NativeReleaseAssessment{}, errors.Join(readErr, closeErr, workErr)
		}
		digest := file.BeforeSHA256()
		if after {
			digest = file.AfterSHA256()
		}
		if readErr != nil || closeErr != nil || mode.Perm() != file.BeforeMode() || journalrecord.Fingerprint(content) != digest {
			return NativeReleaseAssessment{}, errors.Join(fmt.Errorf("native release local file differs from its selected phase"), readErr, closeErr)
		}
	}
	registry, err := options.Registry(ctx)
	if err != nil {
		return NativeReleaseAssessment{}, err
	}
	needsRelease := false
	if after {
		_, needsRelease, err = release.Metadata().ApplyAfterCommit(registry)
	} else {
		err = release.Metadata().RequireBefore(registry)
	}
	if err != nil {
		return NativeReleaseAssessment{}, err
	}
	if err := ValidateNativeRevision(ctx, source.OperationDir(), source.Identity().JournalAuthorityFingerprint(), source.PhysicalAuthority(), options.Filesystem, options.OperationAuthority, options.Budget); err != nil {
		return NativeReleaseAssessment{}, err
	}
	if err := ctx.Err(); err != nil {
		return NativeReleaseAssessment{}, err
	}
	return NativeReleaseAssessment{source: source, after: after, needsRelease: needsRelease, valid: true}, nil
}
