package declaration

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

// CurrentManifestVersion is the manifest schema version emitted and accepted by daem.
const CurrentManifestVersion = contractversion.ManifestSchema

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
		return Manifest{}, fmt.Errorf("unknown manifest key %q", undecoded[0].String())
	}

	return manifest, nil
}

func admitManifestStructure(content []byte) error {
	return tomlstrict.Admit(context.Background(), content, tomlstrict.StandardLimits())
}
