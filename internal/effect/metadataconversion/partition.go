package metadataconversion

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output/ownership"
)

func registryDocuments(registry ownership.Registry) map[pathauthority.Exact][]lookupCandidate {
	documents := make(map[pathauthority.Exact][]lookupCandidate)
	for _, claim := range registry.Claims() {
		documents[claim.Address().PathAuthority()] = nil
	}
	for _, binding := range registry.DocumentBindings() {
		documents[binding.PathAuthority()] = nil
	}
	return documents
}

func (compiler *planCompiler) requireMetadataDocumentSeparation(ctx context.Context, table pathauthority.ReferenceTable, evidenceRoot string) error {
	if len(compiler.documents) == 0 {
		return ctx.Err()
	}
	metadata := make([]string, 0, len(compiler.compiled)+1)
	metadata = append(metadata, evidenceRoot)
	for _, compiled := range compiler.compiled {
		metadata = append(metadata, compiled.target.Path())
	}
	slices.Sort(metadata)
	metadata = slices.Compact(metadata)
	index := metadataExclusionIndex{byKey: make(map[string][]pathauthority.Exact)}
	for _, path := range metadata {
		if err := ctx.Err(); err != nil {
			return err
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, mutationfs.MaximumPhysicalPathDepth, compiler.input.PhysicalWorkBudget)
		if cancellation := ctx.Err(); cancellation != nil {
			return errors.Join(cancellation, err)
		}
		if err != nil {
			return err
		}
		if exact, present := observation.Exact(); present {
			for _, key := range exclusionKeys(exact) {
				index.keys = append(index.keys, key)
				index.byKey[key] = append(index.byKey[key], exact)
			}
		} else {
			provisional, _ := observation.Provisional()
			index.provisional = append(index.provisional, provisional)
		}
	}
	slices.Sort(index.keys)
	index.keys = slices.Compact(index.keys)
	for identity := range compiler.documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		reference, present := table.Reference(identity)
		if !present {
			return fmt.Errorf("managed document lacks complete qualified reference membership")
		}
		if index.conflicts(identity) {
			return fmt.Errorf("managed document %q overlaps a conversion metadata or evidence namespace", reference.Locator())
		}
	}
	return nil
}

type metadataExclusionIndex struct {
	keys        []string
	byKey       map[string][]pathauthority.Exact
	provisional []pathauthority.Provisional
}

func (index metadataExclusionIndex) conflicts(document pathauthority.Exact) bool {
	for _, missing := range index.provisional {
		if missing.CouldOverlap(document) {
			return true
		}
	}
	for _, key := range exclusionKeys(document) {
		for ancestor := key; ; ancestor = filepath.Dir(ancestor) {
			for _, metadata := range index.byKey[ancestor] {
				if metadata.MayOverlap(document) {
					return true
				}
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
		prefix := strings.TrimSuffix(key, string(filepath.Separator)) + string(filepath.Separator)
		start, _ := slices.BinarySearch(index.keys, prefix)
		for next := start; next < len(index.keys) && strings.HasPrefix(index.keys[next], prefix); next++ {
			for _, metadata := range index.byKey[index.keys[next]] {
				if metadata.MayOverlap(document) {
					return true
				}
			}
		}
	}
	return false
}

// Legacy comparison is an exclusion index only; the model's overlap predicate
// makes the decision. Neither projection qualifies an access candidate.
func exclusionKeys(identity pathauthority.Exact) []string {
	keys := []string{identity.Key()}
	if legacy, available := identity.LegacyComparisonKey(); available && legacy != identity.Key() {
		keys = append(keys, legacy)
	}
	return keys
}
