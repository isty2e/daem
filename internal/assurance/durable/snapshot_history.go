package durable

import (
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
)

// WithRecordedDelegateAttempts replaces last-attempt history by semantic
// subject, target, and scope identity while preserving unrelated rows.
func (snapshot Snapshot) WithRecordedDelegateAttempts(
	attempts []durableattempt.DelegateAttempt,
) (Snapshot, error) {
	replacements := make(map[durableattempt.DelegateAttemptKey]durableattempt.DelegateAttempt, len(attempts))
	for _, attempt := range attempts {
		replacements[attempt.SemanticKey()] = attempt
	}

	current := snapshot.DelegateAttempts()
	next := make([]durableattempt.DelegateAttempt, 0, len(current)+len(replacements))
	for _, existing := range current {
		key := existing.SemanticKey()
		if replacement, replace := replacements[key]; replace {
			next = append(next, replacement)
			delete(replacements, key)
			continue
		}
		next = append(next, existing)
	}
	for _, replacement := range replacements {
		next = append(next, replacement)
	}
	return snapshot.WithDelegateAttempts(next)
}

// WithRecordedHostRouteAttempts replaces history by canonical route identity.
// Attempt history never creates, promotes, or removes carrier claims.
func (snapshot Snapshot) WithRecordedHostRouteAttempts(
	attempts []durableattempt.HostRouteAttempt,
) (Snapshot, error) {
	replacements := make(map[durableattempt.HostRouteAttemptKey]durableattempt.HostRouteAttempt, len(attempts))
	for _, attempt := range attempts {
		replacements[attempt.SemanticKey()] = attempt
	}

	current := snapshot.HostRouteAttempts()
	nextAttempts := make([]durableattempt.HostRouteAttempt, 0, len(current)+len(replacements))
	for _, existing := range current {
		key := existing.SemanticKey()
		if replacement, replace := replacements[key]; replace {
			nextAttempts = append(nextAttempts, replacement)
			delete(replacements, key)
			continue
		}
		nextAttempts = append(nextAttempts, existing)
	}
	for _, replacement := range replacements {
		nextAttempts = append(nextAttempts, replacement)
	}
	return snapshot.WithHostRouteAttempts(nextAttempts)
}
