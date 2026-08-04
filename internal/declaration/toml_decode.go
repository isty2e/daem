package declaration

import (
	"fmt"

	burnttoml "github.com/BurntSushi/toml"

	"github.com/isty2e/daem/internal/contractversion"
)

// CurrentManifestVersion is the manifest schema version emitted and accepted by daem.
const CurrentManifestVersion = contractversion.ManifestSchema

func DecodeManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	metadata, err := burnttoml.Decode(string(content), &manifest)
	if err != nil {
		return Manifest{}, err
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return Manifest{}, fmt.Errorf("unknown manifest key %q", undecoded[0].String())
	}

	return manifest, nil
}
