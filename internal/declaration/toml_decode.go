package declaration

import (
	"fmt"

	burnttoml "github.com/BurntSushi/toml"
)

const SupportedManifestVersion = 1

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
