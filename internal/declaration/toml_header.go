package declaration

import (
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
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
	metadata, err := tomlstrict.DecodeAdmitted(context.Background(), content, &header)
	if err != nil {
		return ManifestHeader{}, fmt.Errorf("parse manifest header: %w", PositionManifestKeyError(content, DocumentRange{End: len(content)}, metadata, err))
	}
	return header, nil
}
