// Package targetselection normalizes command-requested target filters into a
// stable target set.
//
// It owns target-set selection only. It may parse requested target names and
// intersect them with manifest-declared target availability. It must not own
// resource invariants, target surface capability policy, workflow sequencing,
// diagnostics, presentation, or CLI formatting.
package targetselection
