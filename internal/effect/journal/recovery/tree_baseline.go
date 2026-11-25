package recovery

import (
	"maps"
	"slices"
)

// TreeBaseline relates two digest formats for the same observed tree. It
// changes only comparisons with live directories, never backup or state identity.
type TreeBaseline struct{ digests map[string]string }

func VerifiedTree(legacy, installed string) TreeBaseline {
	return TreeBaseline{map[string]string{legacy: installed}}
}

func (proof TreeBaseline) Empty() bool { return len(proof.digests) == 0 }

func (proof TreeBaseline) With(other TreeBaseline) TreeBaseline {
	if proof.Empty() {
		return other
	}
	if other.Empty() {
		return proof
	}
	joined := maps.Clone(proof.digests)
	maps.Copy(joined, other.digests)
	return TreeBaseline{joined}
}

// ObservedTreeBaselines retains only pairs matching recorded directories. A
// backup can supply the old executable labels, but its own digest still guards restoration.
func ObservedTreeBaselines(entries []Entry, paths []PathEvidence, backups []BackupEvidence) TreeBaseline {
	wanted := make(map[string]bool)
	for _, entry := range entries {
		if entry.before.Kind == PathKindDirectory {
			wanted[entry.before.ContentHash] = true
		}
		if entry.expectedAfter.Kind == PathKindDirectory {
			wanted[entry.expectedAfter.ContentHash] = true
		}
	}
	if len(wanted) == 0 {
		return TreeBaseline{}
	}
	verified := make(map[string]string)
	add := func(kind, problem string, h ContentEvidence) {
		if kind != PathKindDirectory || problem != "" || h.Installed == "" {
			return
		}
		for _, candidate := range []string{h.Source, h.Alternate} {
			if candidate != "" && wanted[candidate] {
				verified[candidate] = h.Installed
			}
		}
	}
	for _, path := range paths {
		add(path.Kind, path.Error, path.Hashes)
	}
	for _, backup := range backups {
		add(backup.Kind, backup.Error, backup.Hashes)
	}
	return TreeBaseline{verified}
}

func (proof TreeBaseline) Matches(kind, digest string) bool {
	return kind == PathKindDirectory && digest != "" && proof.digests[digest] != ""
}

func (proof TreeBaseline) LiveEntry(entry Entry) Entry {
	if proof.Matches(entry.before.Kind, entry.before.ContentHash) {
		entry.liveBeforeHash = proof.digests[entry.before.ContentHash]
	}
	if proof.Matches(entry.expectedAfter.Kind, entry.expectedAfter.ContentHash) {
		entry.expectedAfter.ContentHash = proof.digests[entry.expectedAfter.ContentHash]
	}
	return entry
}

func (proof TreeBaseline) LiveIntents(intents []RemovalIntent) []RemovalIntent {
	if proof.Empty() {
		return intents
	}
	result := slices.Clone(intents)
	for i, intent := range result {
		intent = intent.clone()
		for j, state := range intent.demand.states {
			if before, ok := state.Before(); ok && proof.Matches(before.Kind, before.ContentHash) {
				intent.demand.states[j].installedTree = proof.digests[before.ContentHash]
			}
			if after, ok := state.Expected(); ok && proof.Matches(after.Kind, after.ContentHash) {
				intent.demand.states[j].installedTree = proof.digests[after.ContentHash]
			}
		}
		result[i] = intent
	}
	return result
}

func (authority Authority) WithTreeBaseline(proof TreeBaseline) Authority {
	if proof.Empty() {
		return authority
	}
	authority.entries = slices.Clone(authority.entries)
	for i, entry := range authority.entries {
		authority.entries[i] = proof.LiveEntry(entry)
	}
	authority.removalIntents = proof.LiveIntents(authority.removalIntents)
	authority.treeBaseline = authority.treeBaseline.With(proof)
	return authority
}

func (plan Plan) TreeBaseline() TreeBaseline { return plan.authority.treeBaseline }
