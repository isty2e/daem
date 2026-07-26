package buildidentity

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/platformsupport"
)

// FromBuildInfo normalizes Go build metadata into one canonical identity.
// Malformed or duplicate owned settings are rejected; absent facts remain
// unknown so development builds can still be represented truthfully.
func FromBuildInfo(info debug.BuildInfo) (Identity, error) {
	settings, err := ownedSettings(info.Settings)
	if err != nil {
		return Identity{}, err
	}

	identity := Identity{
		mainPackage: info.Path,
		mainModule:  info.Main.Path,
		version:     info.Main.Version,
		goVersion:   info.GoVersion,
	}

	if value, ok := settings["vcs"]; ok {
		if value == "" || strings.TrimSpace(value) != value {
			return Identity{}, fmt.Errorf("invalid vcs build setting %q", value)
		}
		identity.vcs = value
	}
	if value, ok := settings["vcs.revision"]; ok {
		if value == "" || strings.TrimSpace(value) != value {
			return Identity{}, fmt.Errorf("invalid vcs.revision build setting %q", value)
		}
		identity.revision = value
	}
	if value, ok := settings["vcs.time"]; ok {
		revisionAt, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return Identity{}, fmt.Errorf("invalid vcs.time build setting %q: %w", value, parseErr)
		}
		_, offset := revisionAt.Zone()
		if offset != 0 {
			return Identity{}, fmt.Errorf("vcs.time build setting must be UTC: %q", value)
		}
		identity.revisionAt = revisionAt.UTC()
		identity.hasRevisionAt = true
	}
	if value, ok := settings["vcs.modified"]; ok {
		switch value {
		case "false":
			identity.sourceState = SourceClean
		case "true":
			identity.sourceState = SourceModified
		default:
			return Identity{}, fmt.Errorf("invalid vcs.modified build setting %q", value)
		}
	}

	goos, hasGOOS := settings["GOOS"]
	goarch, hasGOARCH := settings["GOARCH"]
	if hasGOOS || hasGOARCH {
		validatedOS := goos
		if !hasGOOS {
			validatedOS = "unknown"
		}
		validatedArch := goarch
		if !hasGOARCH {
			validatedArch = "unknown"
		}
		target, targetErr := platformsupport.ParseTarget(validatedOS, validatedArch)
		if targetErr != nil {
			return Identity{}, fmt.Errorf("invalid target build settings: %w", targetErr)
		}
		if hasGOOS && hasGOARCH {
			identity.target = target
		}
	}

	if value, ok := settings["CGO_ENABLED"]; ok {
		switch value {
		case "0":
			identity.cgo = CGODisabled
		case "1":
			identity.cgo = CGOEnabled
		default:
			return Identity{}, fmt.Errorf("invalid CGO_ENABLED build setting %q", value)
		}
	}

	return identity, nil
}

func ownedSettings(settings []debug.BuildSetting) (map[string]string, error) {
	owned := map[string]struct{}{
		"vcs": {}, "vcs.revision": {}, "vcs.time": {}, "vcs.modified": {},
		"GOOS": {}, "GOARCH": {}, "CGO_ENABLED": {},
	}
	values := make(map[string]string, len(owned))
	for _, setting := range settings {
		if _, relevant := owned[setting.Key]; !relevant {
			continue
		}
		if _, duplicate := values[setting.Key]; duplicate {
			return nil, fmt.Errorf("duplicate %s build setting", setting.Key)
		}
		values[setting.Key] = setting.Value
	}
	return values, nil
}
