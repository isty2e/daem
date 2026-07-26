package pipackage

import (
	"fmt"
	"os"
	"path/filepath"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type sourceKind string

const (
	sourceKindNPM   sourceKind = "npm"
	sourceKindGit   sourceKind = "git"
	sourceKindLocal sourceKind = "local"
)

type sourceIdentity struct {
	kind sourceKind
	key  string
}

func sourceIdentityForInput(source string, commandRoot string, scope target.Scope) (sourceIdentity, error) {
	return parseSourceIdentity(source, commandRoot, scope)
}

func sourceIdentityForSettings(source string, settingsBase string, scope target.Scope) (sourceIdentity, error) {
	return parseSourceIdentity(source, settingsBase, scope)
}

func expectedSettingsSource(
	source string,
	commandRoot string,
	settingsBase string,
	scope target.Scope,
) (string, error) {
	identity, err := sourceIdentityForInput(source, commandRoot, scope)
	if err != nil {
		return "", err
	}
	if identity.kind != sourceKindLocal {
		return source, nil
	}
	relative, err := filepath.Rel(settingsBase, identity.key)
	if err != nil {
		return "", fmt.Errorf("derive Pi local settings source: %w", err)
	}
	if relative == "" {
		return ".", nil
	}
	return relative, nil
}

func parseSourceIdentity(source string, base string, scope target.Scope) (sourceIdentity, error) {
	if _, err := validateSourceText(source); err != nil {
		return sourceIdentity{}, err
	}
	ref, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, source)
	if err != nil {
		return sourceIdentity{}, err
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		ref,
	)
	if err != nil {
		return sourceIdentity{}, err
	}
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return sourceIdentity{}, err
	}
	switch interpreted.Class() {
	case extensiontopology.CarrierSourceNPM:
		return sourceIdentity{kind: sourceKindNPM, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceGit:
		return sourceIdentity{kind: sourceKindGit, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceLocal:
	default:
		return sourceIdentity{}, fmt.Errorf("unsupported Pi package source class %q", interpreted.Class())
	}
	root, err := cleanAbsoluteRoot("Pi package source base", base)
	if err != nil {
		return sourceIdentity{}, err
	}
	homeRoot, err := os.UserHomeDir()
	if err != nil {
		return sourceIdentity{}, fmt.Errorf("resolve Pi local package source home: %w", err)
	}
	context, err := extensiontopology.NewLocalSourceContext(root, homeRoot)
	if err != nil {
		return sourceIdentity{}, err
	}
	identity, err := interpreted.ResolveLocal(context)
	if err != nil {
		return sourceIdentity{}, err
	}
	return sourceIdentity{kind: sourceKindLocal, key: identity.Path()}, nil
}
