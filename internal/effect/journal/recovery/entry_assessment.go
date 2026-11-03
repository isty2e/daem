package recovery

import "fmt"

// EntryAssessment contains path facts and guarded actions, not a complete
// recovery authority or permission to finalize metadata or retire a journal.
type EntryAssessment struct {
	before, expected, recoverable bool
	actions                       []Action
	initialized                   bool
}

func (assessment EntryAssessment) MatchesBefore() bool {
	return assessment.initialized && assessment.before
}

func (assessment EntryAssessment) MatchesExpected() bool {
	return assessment.initialized && assessment.expected
}

func (assessment EntryAssessment) Recoverable() bool {
	return assessment.initialized && assessment.recoverable
}

func (assessment EntryAssessment) Actions() []Action {
	return cloneActions(assessment.actions)
}

// ValidateBackups qualifies all referenced originals, including entries whose
// host paths already match Before. It does not classify an operation's phase.
func ValidateBackups(entries []Entry, evidence []BackupEvidence) error {
	backups, err := backupEvidenceIndex(evidence)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := entry.validate(); err != nil {
			return err
		}
		if entry.before.Existed && isContentBackedPathKind(entry.before.Kind) {
			if detail := backupMismatch(entry.before, backups); detail != "" {
				return fmt.Errorf("recovery backup for %q: %s", entry.destination, detail)
			}
		}
	}
	return nil
}
