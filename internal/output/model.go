package output

// Destination is the canonical persisted spelling of a planned portable host
// output address. Validate it before admitting it into a canonical model.
type Destination string

// ContentPath identifies the managed projection within a destination.
// The empty value means the whole destination path is managed.
type ContentPath string
