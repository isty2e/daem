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

// HostLoadIdentityForInput derives Pi's source-class-qualified package
// identity from one manifest-side source and command root.
func HostLoadIdentityForInput(
	source string,
	commandRoot string,
	scope target.Scope,
) (string, error) {
	identity, err := sourceIdentityForInput(source, commandRoot, scope)
	if err != nil {
		return "", err
	}
	return identity.hostLoadIdentity(scope), nil
}

// HostLoadIdentityForContext uses selected lexical roots without consulting the
// process environment or filesystem. It supplies no package or path authority.
func HostLoadIdentityForContext(source string, scope target.Scope, context extensiontopology.LocalSourceContext) (string, error) {
	identity, err := sourceIdentityForContext(source, scope, context)
	if err != nil {
		return "", err
	}
	return identity.hostLoadIdentity(scope), nil
}

func (identity sourceIdentity) hostLoadIdentity(scope target.Scope) string {
	if identity.kind == sourceKindLocal {
		return string(identity.kind) + ":" + string(scope) + ":" + identity.key
	}
	return string(identity.kind) + ":" + identity.key
}

func sourceIdentityForInput(source string, commandRoot string, scope target.Scope) (sourceIdentity, error) {
	return parseSourceIdentity(source, commandRoot, scope)
}

func sourceIdentityForContext(source string, scope target.Scope, context extensiontopology.LocalSourceContext) (sourceIdentity, error) {
	interpreted, err := interpretPackageSource(source, scope)
	if err != nil {
		return sourceIdentity{}, err
	}
	return sourceIdentityForCarrier(interpreted, context)
}

func sourceIdentityForRelation(relation ScopedRelation) (sourceIdentity, error) {
	source := string(relation.key.ExpectedRelation().SubjectKey())
	if relation.sourceContext != nil {
		return sourceIdentityForContext(source, relation.scope, *relation.sourceContext)
	}
	return sourceIdentityForInput(source, relation.commandRoot, relation.scope)
}

func expectedSettingsSource(relation ScopedRelation, settingsBase string) (string, error) {
	identity, err := sourceIdentityForRelation(relation)
	if err != nil {
		return "", err
	}
	if identity.kind != sourceKindLocal {
		return string(relation.key.ExpectedRelation().SubjectKey()), nil
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
	interpreted, err := interpretPackageSource(source, scope)
	if err != nil {
		return sourceIdentity{}, err
	}
	if interpreted.Class() != extensiontopology.CarrierSourceLocal {
		return sourceIdentityForCarrier(interpreted, extensiontopology.LocalSourceContext{})
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
	return sourceIdentityForCarrier(interpreted, context)
}

func interpretPackageSource(source string, scope target.Scope) (extensiontopology.CarrierSource, error) {
	if _, err := validateSourceText(source); err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	ref, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, source)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		ref,
	)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	return extensiontopology.InterpretCarrierSource(key)
}

func sourceIdentityForCarrier(interpreted extensiontopology.CarrierSource, context extensiontopology.LocalSourceContext) (sourceIdentity, error) {
	switch interpreted.Class() {
	case extensiontopology.CarrierSourceNPM:
		return sourceIdentity{kind: sourceKindNPM, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceGit:
		return sourceIdentity{kind: sourceKindGit, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceLocal:
	default:
		return sourceIdentity{}, fmt.Errorf("unsupported Pi package source class %q", interpreted.Class())
	}
	identity, err := interpreted.ResolveLocal(context)
	if err != nil {
		return sourceIdentity{}, err
	}
	return sourceIdentity{kind: sourceKindLocal, key: identity.Path()}, nil
}
