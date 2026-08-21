package declaration

import (
	"fmt"
	"strings"

	burnttoml "github.com/BurntSushi/toml"
)

type ManifestHeader struct {
	Targets  []string `toml:"targets"`
	Defaults Defaults `toml:"defaults"`
}

// EffectiveScope resolves a declaration-local scope against manifest defaults.
func (header ManifestHeader) EffectiveScope(rawScope string) string {
	if strings.TrimSpace(rawScope) != "" {
		return strings.TrimSpace(rawScope)
	}
	if strings.TrimSpace(header.Defaults.Scope) != "" {
		return strings.TrimSpace(header.Defaults.Scope)
	}
	return "project"
}

// EffectiveTargets resolves declaration-local targets against manifest targets.
func (header ManifestHeader) EffectiveTargets(rawTargets []string) []string {
	if len(rawTargets) != 0 {
		return append([]string(nil), rawTargets...)
	}
	return append([]string(nil), header.Targets...)
}

func DecodeManifestHeader(content []byte) (ManifestHeader, error) {
	if err := admitManifestStructure(content); err != nil {
		return ManifestHeader{}, fmt.Errorf("parse manifest header: %w", err)
	}

	var header ManifestHeader
	if _, err := burnttoml.Decode(string(content), &header); err != nil {
		return ManifestHeader{}, fmt.Errorf("parse manifest header: %w", err)
	}
	return header, nil
}
