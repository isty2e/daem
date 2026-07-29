package aggregatecodec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ExtensionOrderIdentityResolver selects the finite host source-identity
// codecs used by extension order classes. Its input must already have passed
// selected-manifest source normalization.
func ExtensionOrderIdentityResolver(
	paths daempaths.Paths,
) func(desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
	return func(carrier desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
		var (
			identity string
			err      error
		)
		switch carrier.Carrier() {
		case desiredextension.CarrierOpenCodePlugin:
			sourcePath, pathErr := openCodeOrderSourcePath(carrier.Scope(), paths)
			if pathErr != nil {
				return "", pathErr
			}
			identity, err = opencodeconfig.HostLoadIdentity(
				carrier.Source().Ref(),
				sourcePath,
			)
		case desiredextension.CarrierPiPackage:
			identity, err = piExtensionOrderIdentity(carrier, paths.ManifestRoot)
		default:
			return "", fmt.Errorf(
				"extension carrier %q has no admitted order identity codec",
				carrier.Carrier(),
			)
		}
		if err != nil {
			return "", err
		}
		return hostrelation.NewHostLoadIdentity(identity)
	}
}

func openCodeOrderSourcePath(
	scope target.Scope,
	paths daempaths.Paths,
) (string, error) {
	var globalRoot string
	if scope == target.ScopeGlobal {
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		var home string
		if xdgConfigHome == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve OpenCode user home: %w", err)
			}
		}
		var err error
		globalRoot, err = opencodeconfig.DefaultGlobalConfigRoot(
			xdgConfigHome,
			home,
		)
		if err != nil {
			return "", err
		}
	}
	directory, err := opencodeconfig.ConfigDirectory(
		paths.ManifestRoot,
		globalRoot,
		scope,
	)
	if err != nil {
		return "", err
	}
	names, err := opencodeconfig.CandidateNames(opencodeconfig.ConfigServer)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, names[0]), nil
}

func piExtensionOrderIdentity(
	carrier desiredextension.CarrierKey,
	manifestRoot string,
) (string, error) {
	source, err := extensiontopology.InterpretCarrierSource(carrier)
	if err != nil {
		return "", err
	}
	switch source.Class() {
	case extensiontopology.CarrierSourceNPM:
		return "npm:" + source.Identity(), nil
	case extensiontopology.CarrierSourceGit:
		return "git:" + source.Identity(), nil
	case extensiontopology.CarrierSourceLocal:
	default:
		return "", fmt.Errorf(
			"Pi extension source class %q has no order identity codec",
			source.Class(),
		)
	}

	localSource := filepath.FromSlash(source.Identity())
	if strings.HasPrefix(localSource, "~") {
		return "", fmt.Errorf(
			"Pi local extension source %q was not normalized at manifest ingress",
			source.Identity(),
		)
	}
	switch carrier.Scope() {
	case target.ScopeGlobal:
		if !filepath.IsAbs(localSource) {
			return "", fmt.Errorf(
				"Pi global local extension source %q must be absolute after manifest normalization",
				source.Identity(),
			)
		}
	case target.ScopeProject:
		if filepath.IsAbs(localSource) {
			return "", fmt.Errorf(
				"Pi project local extension source %q must be manifest-relative after normalization",
				source.Identity(),
			)
		}
	default:
		return "", fmt.Errorf("Pi extension scope %q has no order identity codec", carrier.Scope())
	}

	context, err := extensiontopology.NewLocalSourceContext(manifestRoot, manifestRoot)
	if err != nil {
		return "", err
	}
	localIdentity, err := source.ResolveLocal(context)
	if err != nil {
		return "", err
	}
	return "local:" + string(carrier.Scope()) + ":" + localIdentity.Path(), nil
}
