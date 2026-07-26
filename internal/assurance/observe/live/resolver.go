package live

import "github.com/isty2e/daem/internal/output"

// DestinationResolver resolves one canonical destination at the host boundary.
type DestinationResolver func(destination output.Destination) (string, error)
