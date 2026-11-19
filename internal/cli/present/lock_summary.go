package clipresent

import (
	"fmt"
	"io"

	lock "github.com/isty2e/daem/internal/realization/lock"
)

type LockSummaryInput struct {
	Heading      string
	LockfilePath string
	Lockfile     lock.File
	Delta        *lock.Delta
	Unavailable  string
	DryRun       bool
	NextCommand  string
}

func PrintLockSummary(output io.Writer, input LockSummaryInput, options HumanOptions) {
	subjects := input.Lockfile.Locked.Subjects()

	fmt.Fprintf(output, "%s: %s\n", input.Heading, Escape(input.LockfilePath))
	if input.DryRun || input.Delta == nil {
		fmt.Fprintf(output, "lock entries: %d\n", len(subjects))
	}
	if input.Delta != nil {
		PrintDeltaSummaryWithOptions(output, *input.Delta, options)
	} else {
		if orderCount := input.Lockfile.Locked.OrderLen(); orderCount != 0 {
			fmt.Fprintf(output, "lock order constraints: %d\n", orderCount)
		}
		fmt.Fprintln(output, input.Unavailable)
	}
	if !input.DryRun {
		return
	}
	if options.Verbose {
		printLockedSubjectIDs(output, subjects)
	}
	if repaired := repairedSubjectCount(subjects); repaired > 0 {
		fmt.Fprintf(output, "repaired lock entries: %d\n", repaired)
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
