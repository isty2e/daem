package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/reconcile"
)

// PrintReconciliationGuidance reports whether selected recovery guidance was emitted.
func PrintReconciliationGuidance(output io.Writer, result reconcile.Result) bool {
	var drift, unmanaged, pinChange bool
	for _, decision := range result.Decisions() {
		var reason reconcile.ActionReason
		if managed, ok := decision.ManagedPath(); ok && managed.IsBlocked() {
			reason = managed.Reason()
		} else if aggregate, ok := decision.Aggregate(); ok && aggregate.IsBlocked() {
			reason = aggregate.Reason()
		}
		switch reason {
		case reconcile.ReasonDriftedOutput:
			drift = true
		case reconcile.ReasonUnmanagedOutputExists:
			unmanaged = true
		}
	}
	for _, action := range result.Relations() {
		_, hasTransition := action.PinTransition()
		pinChange = pinChange || hasTransition || action.Reason() == reconcile.ReasonPinTransitionConflict
	}

	if drift {
		fmt.Fprintln(output, "drift: preserve the local edits before choosing a direction; --manage-existing does not override drift")
		fmt.Fprintln(output, "  to keep an intentional edit at the same destination, align its source or declaration, run lock, then preview apply again")
		fmt.Fprintln(output, "  to discard it, first preserve a copy and independently restore the known managed baseline; daem has no automatic baseline restore")
		fmt.Fprintln(output, "  blocked plans do not produce content diffs; inspect the destination and compare reviewed copies outside daem")
	}
	if unmanaged {
		fmt.Fprintln(output, "unmanaged content is preserved: compare the selected destination or config entry with its declaration before changing either")
		fmt.Fprintln(output, "  --manage-existing can adopt an eligible exact match, not overwrite a mismatch; preview it and review any other disclosed effects")
	}
	if pinChange {
		fmt.Fprintln(output, "pin changes: for a retained attempt, preserve the original pending target in the manifest and lock; inspect scoped settings and conflicting consumers")
		fmt.Fprintln(output, "  retry only after a fresh apply --dry-run with the same selection and new authorization; settings alone do not finish the attempt, and recover cannot roll back Pi")
	}
	if drift || unmanaged || pinChange {
		fmt.Fprintln(output, "help: https://github.com/isty2e/daem/blob/main/docs/troubleshooting.md")
		return true
	}
	return false
}
