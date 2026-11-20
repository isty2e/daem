package piorder

import (
	"fmt"
	"path/filepath"

	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"

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

type SourceIdentity struct {
	kind sourceKind
	key  string
}

// HostLoadIdentityForContext uses selected lexical roots without consulting the
// process environment or filesystem. It supplies no package or path authority.
func HostLoadIdentityForContext(source string, scope target.Scope, context extensiontopology.LocalSourceContext) (string, error) {
	identity, err := sourceIdentityForContext(source, scope, context)
	if err != nil {
		return "", err
	}
	return identity.HostLoadIdentity(scope), nil
}

func (identity SourceIdentity) HostLoadIdentity(scope target.Scope) string {
	if identity.kind == sourceKindLocal {
		return string(identity.kind) + ":" + string(scope) + ":" + identity.key
	}
	return string(identity.kind) + ":" + identity.key
}

func sourceIdentityForContext(source string, scope target.Scope, context extensiontopology.LocalSourceContext) (SourceIdentity, error) {
	interpreted, err := InterpretPackageSource(source, scope)
	if err != nil {
		return SourceIdentity{}, err
	}
	return ResolveSourceIdentity(interpreted, context)
}

func sourceIdentityForRelation(relation ScopedRelation) (SourceIdentity, error) {
	source := string(relation.key.ExpectedRelation().SubjectKey())
	return sourceIdentityForContext(source, relation.scope, relation.sourceContext)
}

func expectedSettingsSource(relation ScopedRelation, settingsBase string) (string, error) {
	identity, err := sourceIdentityForRelation(relation)
	if err != nil {
		return "", err
	}
	return identity.SettingsSource(string(relation.key.ExpectedRelation().SubjectKey()), settingsBase)
}

func (identity SourceIdentity) LocalPath() string {
	if identity.kind == sourceKindLocal {
		return identity.key
	}
	return ""
}

func (identity SourceIdentity) SettingsSource(source, settingsBase string) (string, error) {
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

func InterpretPackageSource(source string, scope target.Scope) (extensiontopology.CarrierSource, error) {
	if err := piconfig.ValidatePackageSource(source); err != nil {
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

func ResolveSourceIdentity(interpreted extensiontopology.CarrierSource, context extensiontopology.LocalSourceContext) (SourceIdentity, error) {
	switch interpreted.Class() {
	case extensiontopology.CarrierSourceNPM:
		return SourceIdentity{kind: sourceKindNPM, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceGit:
		return SourceIdentity{kind: sourceKindGit, key: interpreted.Identity()}, nil
	case extensiontopology.CarrierSourceLocal:
	default:
		return SourceIdentity{}, fmt.Errorf("unsupported Pi package source class %q", interpreted.Class())
	}
	identity, err := interpreted.ResolveLocal(context)
	if err != nil {
		return SourceIdentity{}, err
	}
	return SourceIdentity{kind: sourceKindLocal, key: identity.Path()}, nil
}
