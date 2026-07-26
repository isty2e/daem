// Package entity owns stable authored desired-entity identity.
//
// It owns the closed current family kind set, validated IDs, canonical text
// encoding, and deterministic ordering. It does not own family fields, target
// bindings, source outcomes, lowered subjects, host paths, or operation IDs.
// The package is separate from desired's aggregate root so desired family
// packages can share identity without creating a Go import cycle.
package entity
