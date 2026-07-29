package pipackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	artifactaccess "github.com/isty2e/daem/internal/supply/artifact/access"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const (
	maximumPackageManifestBytes = 1 << 20
	maximumPackageManifestDepth = 16
)

// VersionState classifies the precision of one current installed-package
// version observation.
type VersionState string

const (
	VersionExact        VersionState = "exact"
	VersionAbsent       VersionState = "absent"
	VersionUnobservable VersionState = "unobservable"
)

// VersionObservation is current, redaction-safe package identity evidence.
// It grants no carrier relation, profile admission, or runtime authority.
type VersionObservation struct {
	state       VersionState
	packageName string
	version     string
	path        string
	detail      string
}

// State returns whether exact current package metadata was observed.
func (observation VersionObservation) State() VersionState { return observation.state }

// PackageName returns the exact package.json name when State is VersionExact.
func (observation VersionObservation) PackageName() string { return observation.packageName }

// Version returns the exact package.json version when State is VersionExact.
func (observation VersionObservation) Version() string { return observation.version }

// Path returns the package directory inspected for this observation.
func (observation VersionObservation) Path() string { return observation.path }

// Detail returns a redaction-safe failure category for unobservable evidence.
func (observation VersionObservation) Detail() string { return observation.detail }

// ObserveNPMVersion reads the exact current package manifest through the
// no-follow artifact boundary. Missing artifacts are current absence; unsafe
// or malformed artifacts are current unobservable evidence.
func ObserveNPMVersion(
	ctx context.Context,
	inventory Inventory,
	carrier extensiontopology.Carrier,
) VersionObservation {
	if ctx == nil {
		return unobservableVersion("", "observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return unobservableVersion("", "observation context is canceled")
	}
	if err := carrier.Validate(); err != nil {
		return unobservableVersion("", "carrier identity is invalid")
	}
	source, err := extensiontopology.InterpretCarrierSource(carrier.Key())
	if err != nil || source.Class() != extensiontopology.CarrierSourceNPM {
		return unobservableVersion("", "carrier is not an observable npm package")
	}
	artifactPath, baseExists, err := managedArtifactPath(inventory, source)
	if err != nil {
		return unobservableVersion("", "package install path cannot be resolved")
	}
	if !baseExists {
		return VersionObservation{
			state: VersionAbsent, packageName: source.Identity(), path: artifactPath,
		}
	}
	view, err := artifactaccess.OpenNoFollowView(artifactPath)
	if errors.Is(err, fs.ErrNotExist) {
		return VersionObservation{
			state: VersionAbsent, packageName: source.Identity(), path: artifactPath,
		}
	}
	if err != nil {
		return unobservableVersion(artifactPath, "package directory is unsafe or unreadable")
	}
	content, err := view.ReadFile(ctx, "package.json", maximumPackageManifestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return unobservableVersion(artifactPath, "package manifest is missing")
	}
	if err != nil {
		return unobservableVersion(artifactPath, "package manifest is unsafe or unreadable")
	}
	name, version, err := decodePackageManifest(content.Bytes())
	if err != nil {
		return unobservableVersion(artifactPath, "package manifest is malformed")
	}
	if name != source.Identity() {
		return unobservableVersion(artifactPath, "package manifest name does not match the declared package")
	}
	return VersionObservation{
		state:       VersionExact,
		packageName: name,
		version:     version,
		path:        artifactPath,
	}
}

func decodePackageManifest(content []byte) (string, string, error) {
	if err := jsonstrict.Validate(
		content,
		"Pi installed npm package manifest",
		maximumPackageManifestDepth,
	); err != nil {
		return "", "", err
	}
	var document map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&document); err != nil {
		return "", "", err
	}
	var name string
	if err := json.Unmarshal(document["name"], &name); err != nil || name == "" {
		return "", "", fmt.Errorf("package name is required")
	}
	var version string
	if err := json.Unmarshal(document["version"], &version); err != nil || version == "" {
		return "", "", fmt.Errorf("package version is required")
	}
	return name, version, nil
}

func unobservableVersion(path string, detail string) VersionObservation {
	return VersionObservation{
		state:  VersionUnobservable,
		path:   path,
		detail: detail,
	}
}
