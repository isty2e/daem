package adopt

import (
	"errors"
	"fmt"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
)

type nothingToImportError struct {
	message string
	skipped []adoptmodel.Skipped
}

func newNothingToImportError(scans []adoptmodel.Scan, skipped []adoptmodel.Skipped) error {
	return &nothingToImportError{
		message: adoptmodel.ErrNothingToImport.Error() + scanSummary(scans) + skippedSummary(skipped),
		skipped: append([]adoptmodel.Skipped(nil), skipped...),
	}
}

func (err *nothingToImportError) Error() string {
	return err.message
}

func (err *nothingToImportError) Unwrap() error {
	return adoptmodel.ErrNothingToImport
}

type skippedObservationError struct {
	cause   error
	skipped []adoptmodel.Skipped
}

func newSkippedObservationError(cause error, skipped []adoptmodel.Skipped) error {
	return &skippedObservationError{
		cause:   cause,
		skipped: append([]adoptmodel.Skipped(nil), skipped...),
	}
}

func wrapSkippedObservationError(err error, collector *adoptmodel.SkippedCollector) error {
	if !errors.Is(err, adoptmodel.ErrSkipObservationLimitExceeded) {
		return err
	}
	return newSkippedObservationError(err, collector.Skipped())
}

func (err *skippedObservationError) Error() string {
	if err == nil || err.cause == nil {
		return adoptmodel.ErrSkipObservationLimitExceeded.Error()
	}
	return err.cause.Error()
}

func (err *skippedObservationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// ImportFailureSkipped returns immutable typed skip evidence and whether an
// operation-wide skip budget omitted one or more later rows.
func ImportFailureSkipped(err error) ([]adoptmodel.Skipped, bool) {
	var nothing *nothingToImportError
	if errors.As(err, &nothing) {
		return append([]adoptmodel.Skipped(nil), nothing.skipped...), false
	}
	var exhausted *skippedObservationError
	if errors.As(err, &exhausted) {
		return append([]adoptmodel.Skipped(nil), exhausted.skipped...), true
	}
	return nil, false
}

func skippedSummary(skipped []adoptmodel.Skipped) string {
	if len(skipped) == 0 {
		return ""
	}

	var actionRequired int
	var unsupported int
	var informational int
	for _, value := range skipped {
		switch value.Category() {
		case adoptmodel.SkipCategoryActionRequired:
			actionRequired++
		case adoptmodel.SkipCategoryUnsupported:
			unsupported++
		case adoptmodel.SkipCategoryInformational:
			informational++
		}
	}
	return fmt.Sprintf(
		" (skipped action_required=%d unsupported=%d informational=%d)",
		actionRequired,
		unsupported,
		informational,
	)
}

func scanSummary(scans []adoptmodel.Scan) string {
	if len(scans) == 0 {
		return ""
	}

	var entries int
	var imported int
	var skipped int
	for _, scan := range scans {
		entries += scan.Entries
		imported += scan.Imported
		skipped += scan.Skipped
	}
	return fmt.Sprintf(
		" (scanned roots=%d entries=%d imported=%d skipped=%d)",
		len(scans),
		entries,
		imported,
		skipped,
	)
}
