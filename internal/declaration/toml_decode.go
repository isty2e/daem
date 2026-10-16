package declaration

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

// CurrentManifestVersion is the manifest schema version emitted and accepted by daem.
const CurrentManifestVersion int64 = contractversion.ManifestSchema

// UnknownManifestKeyError identifies a key outside the manifest schema.
type UnknownManifestKeyError struct {
	cause  error
	line   int
	column int
}

func (err UnknownManifestKeyError) Error() string { return err.cause.Error() }
func (err UnknownManifestKeyError) Unwrap() error { return err.cause }

// Position identifies the authored key expression without changing error text.
func (err UnknownManifestKeyError) Position() (line, column int, ok bool) {
	return err.line, err.column, err.line > 0 && err.column > 0
}

func DecodeManifest(content []byte) (Manifest, error) {
	if err := admitManifestStructure(content); err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	metadata, err := tomlstrict.DecodeAdmitted(context.Background(), content, &manifest)
	if err != nil {
		return Manifest{}, err
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		key := undecoded[0]
		line, column, _ := tomlstrict.KeyPosition(content, metadata, key)
		return Manifest{}, UnknownManifestKeyError{
			cause:  fmt.Errorf("unknown manifest key %q", key.String()),
			line:   line,
			column: column,
		}
	}

	return manifest, nil
}

func admitManifestStructure(content []byte) error {
	return tomlstrict.Admit(context.Background(), content, tomlstrict.StandardLimits())
}
