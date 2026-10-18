package probe

import (
	"fmt"

	"github.com/isty2e/daem/internal/target"
)

// NoAdmittedBindingError retains the query when no runtime-probe row matches.
type NoAdmittedBindingError struct {
	Name   string
	Target target.Target
	Scope  target.Scope
}

func (failure NoAdmittedBindingError) Error() string {
	return fmt.Sprintf(
		"mcp_server %q has no admitted runtime-probe row matching target=%s scope=%s",
		failure.Name, failure.Target, failure.Scope,
	)
}

// AmbiguousBindingError reports multiple admitted rows for the requested name.
type AmbiguousBindingError struct {
	Name    string
	Matches int
}

func (failure AmbiguousBindingError) Error() string {
	return fmt.Sprintf(
		"mcp_server %q is ambiguous across %d runtime-probe rows; narrow with --target and --scope",
		failure.Name, failure.Matches,
	)
}

// BindingAdmissionError retains the sole rejected row's original cause.
type BindingAdmissionError struct {
	Name string
	Err  error
}

func (failure BindingAdmissionError) Error() string {
	return fmt.Sprintf("mcp_server %q runtime-probe admission: %v", failure.Name, failure.Err)
}

func (failure BindingAdmissionError) Unwrap() error { return failure.Err }
