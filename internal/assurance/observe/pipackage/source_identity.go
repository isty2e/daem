package pipackage

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

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
	return identity.HostLoadIdentity(scope), nil
}

func sourceIdentityForInput(source string, commandRoot string, scope target.Scope) (piorder.SourceIdentity, error) {
	return parseSourceIdentity(source, commandRoot, scope)
}

func sourceIdentityForRelation(relation ScopedRelation) (piorder.SourceIdentity, error) {
	source := string(relation.key.ExpectedRelation().SubjectKey())
	return sourceIdentityForInput(source, relation.commandRoot, relation.scope)
}

func expectedSettingsSource(relation ScopedRelation, settingsBase string) (string, error) {
	identity, err := sourceIdentityForRelation(relation)
	if err != nil {
		return "", err
	}
	return identity.SettingsSource(string(relation.key.ExpectedRelation().SubjectKey()), settingsBase)
}

func parseSourceIdentity(source string, base string, scope target.Scope) (piorder.SourceIdentity, error) {
	interpreted, err := piorder.InterpretPackageSource(source, scope)
	if err != nil {
		return piorder.SourceIdentity{}, err
	}
	if interpreted.Class() != extensiontopology.CarrierSourceLocal {
		return piorder.ResolveSourceIdentity(interpreted, extensiontopology.LocalSourceContext{})
	}
	root, err := cleanAbsoluteRoot("Pi package source base", base)
	if err != nil {
		return piorder.SourceIdentity{}, err
	}
	homeRoot, err := os.UserHomeDir()
	if err != nil {
		return piorder.SourceIdentity{}, fmt.Errorf("resolve Pi local package source home: %w", err)
	}
	context, err := extensiontopology.NewLocalSourceContext(root, homeRoot)
	if err != nil {
		return piorder.SourceIdentity{}, err
	}
	return piorder.ResolveSourceIdentity(interpreted, context)
}
