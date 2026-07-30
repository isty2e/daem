package rootedpath

// DirectoryMountBoundary is an operation-local mount identity captured from
// one open directory. It grants no path-selection or mutation authority.
type DirectoryMountBoundary struct {
	mount identityToken
}

func (boundary DirectoryMountBoundary) valid() bool {
	return boundary.mount != (identityToken{})
}
