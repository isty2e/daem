package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

type requalificationLeaseProbe struct {
	calls     []string
	documents bool
	metadata  bool
	cause     error
}

func (probe *requalificationLeaseProbe) CoversPhysicalAuthority(mutation.PhysicalAuthoritySet) (bool, error) {
	probe.calls = append(probe.calls, "documents")
	return probe.documents, probe.cause
}

func (probe *requalificationLeaseProbe) MatchCurrentPhysicalRequests(context.Context, ...mutation.PhysicalAuthorityRequest) (bool, error) {
	probe.calls = append(probe.calls, "metadata")
	return probe.metadata, probe.cause
}

func TestPostEffectRequalificationChecksEveryPhase(t *testing.T) {
	for _, phase := range []applyRequalificationPhase{requalificationAfterNativePrefix, requalificationAfterHostRoutes, requalificationAfterProviders, requalificationAfterDelegates} {
		label, err := phase.label()
		if err != nil {
			t.Fatal(err)
		}
		for _, fault := range []string{"none", "documents", "metadata", "error"} {
			t.Run(label+"/"+fault, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "current")
				if err := os.WriteFile(path, []byte("settled"), 0o600); err != nil {
					t.Fatal(err)
				}
				fresh := applyAuthorityEvidence{firstEffectRevisions: []mutation.RevisionRequest{mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectReferent)}}
				probe := &requalificationLeaseProbe{documents: true, metadata: true}
				wantCalls := []string{"documents", "metadata"}
				if fault == "documents" {
					probe.documents = false
					wantCalls = wantCalls[:1]
				}
				if fault == "metadata" {
					probe.metadata = false
				}
				if fault == "error" {
					probe.cause = errors.New("lease observation unavailable")
					wantCalls = wantCalls[:1]
				}
				revisions, err := qualifyPostEffectAuthority(t.Context(), phase, applyAuthorityEvidence{}, fresh, probe, true)
				if !reflect.DeepEqual(probe.calls, wantCalls) {
					t.Fatalf("phase checks=%v want=%v", probe.calls, wantCalls)
				}
				if fault != "none" {
					var stale mutation.StalePlanError
					if !errors.As(err, &stale) || probe.cause != nil && !errors.Is(err, probe.cause) {
						t.Fatal("authority refusal lost its classification/cause", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if matches, err := revisions.MatchesCurrent(t.Context()); err != nil || !matches {
					t.Fatal("current phase revisions were not captured", err)
				}
				if err := os.WriteFile(path, []byte("later"), 0o600); err != nil {
					t.Fatal(err)
				}
				if matches, err := revisions.MatchesCurrent(t.Context()); err != nil || matches {
					t.Fatal("new phase revision evidence failed to detect a later change", err)
				}
			})
		}
	}
}

func TestPostEffectRequalificationUsesExplicitAuthorityBasis(t *testing.T) {
	root := t.TempDir()
	builder := operationplan.NewBuilder(operationplan.RevisionsOff, nil, 0)
	if err := builder.AddPhysicalPair(filepath.Join(root, "new"), mutation.AccessExclusive, "pi", "project"); err != nil {
		t.Fatal(err)
	}
	fresh := applyAuthorityEvidence{
		facts:                builder.Compile().Facts(),
		firstEffectRevisions: []mutation.RevisionRequest{mutation.NewBoundedContentRevisionRequest(filepath.Join(root, "metadata"), mutation.PathEffectDirectoryEntry)},
	}
	for _, phase := range []applyRequalificationPhase{requalificationAfterNativePrefix, requalificationAfterHostRoutes, requalificationAfterProviders, requalificationAfterDelegates} {
		probe := &requalificationLeaseProbe{documents: true, metadata: true}
		_, err := qualifyPostEffectAuthority(t.Context(), phase, applyAuthorityEvidence{}, fresh, probe, false)
		if phase == requalificationAfterProviders {
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) || len(probe.calls) != 0 {
				t.Fatal("provider widened stable facts", err, probe.calls)
			}
		} else if err != nil || !reflect.DeepEqual(probe.calls, []string{"documents", "metadata"}) {
			t.Fatal("settled identity basis skipped common lease checks", err, probe.calls)
		}
	}
	probe := &requalificationLeaseProbe{documents: true, metadata: true}
	if _, err := qualifyPostEffectAuthority(t.Context(), 0, applyAuthorityEvidence{}, fresh, probe, false); err == nil || len(probe.calls) != 0 {
		t.Fatal("unknown phase reached authority checks")
	}
}
