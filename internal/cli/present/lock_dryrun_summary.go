package clipresent

import (
	"fmt"
	"io"

	lock "github.com/isty2e/daem/internal/realization/lock"
)

// DryRunSummaryInput is the stable human-readable lock dry-run summary contract.
type DryRunSummaryInput struct {
	LockfilePath string
	Lockfile     lock.File
	Delta        lock.Delta
	NextCommand  string
}

func PrintDryRunSummaryWithOptions(output io.Writer, input DryRunSummaryInput, options HumanOptions) {
	subjects := input.Lockfile.Locked.Subjects()

	fmt.Fprintf(output, "would write lockfile: %s\n", Escape(input.LockfilePath))
	fmt.Fprintf(output, "lockfile entries: subjects=%d\n", len(subjects))
	PrintDeltaSummaryWithOptions(output, input.Delta, options)
	if options.Verbose {
		printLockedSubjectIDs(output, subjects)
	}
	if repaired := repairedSubjectCount(subjects); repaired > 0 {
		fmt.Fprintf(output, "repaired resources: %d\n", repaired)
	}
	if input.NextCommand != "" {
		fmt.Fprintf(output, "next: run %s\n", input.NextCommand)
	}
}

func repairedSubjectCount(subjects []lock.LockedSubjectContract) int {
	count := 0
	for _, subject := range subjects {
		if _, ok := subject.RepairRecipe(); ok {
			count++
		}
	}
	return count
}

func printLockedSubjectIDs(output io.Writer, subjects []lock.LockedSubjectContract) {
	if len(subjects) == 0 {
		return
	}

	fmt.Fprintln(output, "locked.subject:")
	for _, contract := range subjects {
		subject := contract.SubjectID()
		suffix := ""
		if _, ok := contract.RepairRecipe(); ok {
			suffix = " (repaired)"
		}
		fmt.Fprintf(output, "  - %s/%s/%s%s\n", subject.Kind(), subject.Namespace(), subject.Key(), suffix)
	}
}
