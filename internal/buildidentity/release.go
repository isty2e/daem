package buildidentity

import (
	"fmt"
	goversion "go/version"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// NewReleaseRequirement validates the external facts selected by one release
// lane and returns their canonical immutable value.
func NewReleaseRequirement(version string, revision string, goVersion string) (ReleaseRequirement, error) {
	requirement := ReleaseRequirement{version: version, revision: revision, goVersion: goVersion}
	if err := validateReleaseRequirement(requirement); err != nil {
		return ReleaseRequirement{}, err
	}
	return requirement, nil
}

// RequireReleaseFacts rejects an identity that does not contain and exactly
// match every intrinsic release fact. Product-platform admission is deliberately
// outside this method.
func (identity Identity) RequireReleaseFacts(requirement ReleaseRequirement) error {
	if err := validateReleaseRequirement(requirement); err != nil {
		return err
	}
	if identity.mainPackage != MainPackagePath {
		return fmt.Errorf("main package is %q, want %q", known(identity.mainPackage), MainPackagePath)
	}
	if identity.mainModule != MainModulePath {
		return fmt.Errorf("main module is %q, want %q", known(identity.mainModule), MainModulePath)
	}
	if err := validateReleaseVersion(identity.version); err != nil {
		return fmt.Errorf("embedded version is not release-eligible: %w", err)
	}
	if identity.version != requirement.version {
		return fmt.Errorf("embedded version is %q, want %q", identity.version, requirement.version)
	}
	if identity.vcs != "git" {
		return fmt.Errorf("vcs is %q, want %q", known(identity.vcs), "git")
	}
	if err := validateGitRevision(identity.revision); err != nil {
		return fmt.Errorf("embedded revision is not release-eligible: %w", err)
	}
	if identity.revision != requirement.revision {
		return fmt.Errorf("embedded revision is %q, want %q", identity.revision, requirement.revision)
	}
	if !identity.hasRevisionAt {
		return fmt.Errorf("revision time is unknown")
	}
	if identity.sourceState != SourceClean {
		return fmt.Errorf("source state is %s, want clean", identity.sourceState)
	}
	if identity.goVersion != requirement.goVersion {
		return fmt.Errorf("Go version is %q, want %q", known(identity.goVersion), requirement.goVersion)
	}
	if identity.target.OS() == "" || identity.target.Arch() == "" {
		return fmt.Errorf("build target is unknown")
	}
	if identity.cgo != CGODisabled {
		return fmt.Errorf("CGO is %s, want disabled", identity.cgo)
	}
	return nil
}

func validateReleaseRequirement(requirement ReleaseRequirement) error {
	if err := validateReleaseVersion(requirement.version); err != nil {
		return fmt.Errorf("invalid expected version: %w", err)
	}
	if err := validateGitRevision(requirement.revision); err != nil {
		return fmt.Errorf("invalid expected revision: %w", err)
	}
	if strings.TrimSpace(requirement.goVersion) != requirement.goVersion || !goversion.IsValid(requirement.goVersion) {
		return fmt.Errorf("invalid expected Go version %q", requirement.goVersion)
	}
	return nil
}

func validateReleaseVersion(version string) error {
	if !semver.IsValid(version) {
		return fmt.Errorf("version %q is not a v-prefixed semantic version", version)
	}
	if semver.Build(version) != "" {
		return fmt.Errorf("version %q contains build metadata", version)
	}
	if semver.Canonical(version) != version {
		return fmt.Errorf("version %q is not canonical", version)
	}
	if module.IsPseudoVersion(version) {
		return fmt.Errorf("version %q is a pseudo-version", version)
	}
	return nil
}

func validateGitRevision(revision string) error {
	if len(revision) != 40 {
		return fmt.Errorf("revision %q is not a full 40-character Git object id", revision)
	}
	for _, character := range revision {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("revision %q is not lowercase hexadecimal", revision)
		}
	}
	return nil
}

func known(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
