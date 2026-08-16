package pipackage

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode"

	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	artifactaccess "github.com/isty2e/daem/internal/supply/artifact/access"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func observeManagedArtifactAbsence(
	inventory Inventory,
	source extensiontopology.CarrierSource,
) observepostcondition.EvidenceState {
	artifactPath, baseExists, err := managedArtifactPath(inventory, source)
	if err != nil {
		return observepostcondition.EvidenceUnavailable
	}
	if !baseExists {
		return observepostcondition.EvidenceSatisfied
	}
	_, err = artifactaccess.OpenNoFollowView(artifactPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return observepostcondition.EvidenceSatisfied
	case err != nil:
		return observepostcondition.EvidenceUnavailable
	default:
		return observepostcondition.EvidenceUnsatisfied
	}
}

func managedArtifactPath(
	inventory Inventory,
	source extensiontopology.CarrierSource,
) (string, bool, error) {
	base, err := filepath.EvalSymlinks(inventory.settingsBase)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var relative string
	switch source.Class() {
	case extensiontopology.CarrierSourceNPM:
		parts, err := npmInstallParts(source.Identity())
		if err != nil {
			return "", true, err
		}
		relative = filepath.Join(append([]string{"npm", "node_modules"}, parts...)...)
	case extensiontopology.CarrierSourceGit:
		gitPath, err := gitInstallRelative(source.Identity())
		if err != nil {
			return "", true, err
		}
		relative = filepath.Join("git", gitPath)
	default:
		return "", true, fmt.Errorf(
			"Pi source class %q has no managed artifact path",
			source.Class(),
		)
	}
	target := filepath.Join(base, relative)
	within, err := filepath.Rel(base, target)
	if err != nil ||
		within == ".." ||
		strings.HasPrefix(within, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(within) {
		return "", true, fmt.Errorf("Pi managed artifact path escapes its install root")
	}
	return target, true, nil
}

func gitInstallRelative(identity string) (string, error) {
	host, repositoryPath, present := strings.Cut(identity, "/")
	if !present ||
		!validGitHost(host) ||
		repositoryPath == "" ||
		strings.IndexFunc(identity, gitIdentityHasUnsafeControl) >= 0 {
		return "", fmt.Errorf("Pi git package identity %q is not path-safe", identity)
	}
	return filepath.FromSlash(identity), nil
}

func validGitHost(host string) bool {
	return desiredextension.PathSafeGitHost(host)
}

func gitIdentityHasUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func npmInstallParts(identity string) ([]string, error) {
	parts := strings.Split(identity, "/")
	if len(parts) == 1 && validInstallPart(parts[0]) {
		return parts, nil
	}
	if len(parts) == 2 &&
		strings.HasPrefix(parts[0], "@") &&
		validInstallPart(strings.TrimPrefix(parts[0], "@")) &&
		validInstallPart(parts[1]) {
		return parts, nil
	}
	return nil, fmt.Errorf("Pi npm package identity %q is not path-safe", identity)
}

func validInstallPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if character < ' ' ||
			character == 0x7f ||
			character == filepath.Separator ||
			character == '\\' ||
			character == '/' {
			return false
		}
	}
	return true
}
