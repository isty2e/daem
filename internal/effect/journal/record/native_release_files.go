package record

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

// NativeReleaseFileRole selects one independently resolved local metadata file.
// A recorded role never supplies a filesystem path or a host-output grant.
type NativeReleaseFileRole string

const (
	NativeReleaseManifest    NativeReleaseFileRole = "manifest"
	NativeReleaseDefaultLock NativeReleaseFileRole = "default-lock"
	NativeReleaseState       NativeReleaseFileRole = "state"
)

type NativeReleaseFileInput struct {
	Role         NativeReleaseFileRole
	Target       NativeReleaseNamespace
	BeforeExists bool
	BeforeSHA256 string
	BeforeMode   os.FileMode
	AfterSHA256  string
	Write        bool
}

// NativeReleaseFileIntent preserves exact file-set image facts. The state role
// is the one commit point; missing authoring files remain absent.
type NativeReleaseFileIntent struct {
	role         NativeReleaseFileRole
	target       NativeReleaseNamespace
	beforeExists bool
	beforeSHA256 string
	beforeMode   os.FileMode
	afterSHA256  string
	write        bool
}

func NewNativeReleaseFileIntent(input NativeReleaseFileInput) (NativeReleaseFileIntent, error) {
	intent := NativeReleaseFileIntent{
		role: input.Role, target: input.Target, beforeExists: input.BeforeExists,
		beforeSHA256: input.BeforeSHA256, beforeMode: input.BeforeMode, afterSHA256: input.AfterSHA256, write: input.Write,
	}
	if err := intent.validate(); err != nil {
		return NativeReleaseFileIntent{}, err
	}
	return intent, nil
}

func (intent NativeReleaseFileIntent) validate() error {
	if err := intent.target.value.validate(); err != nil {
		return err
	}
	switch intent.role {
	case NativeReleaseManifest, NativeReleaseDefaultLock, NativeReleaseState:
	default:
		return fmt.Errorf("native release has an unknown local metadata role")
	}
	if intent.beforeExists {
		if intent.target.value.Kind != "exact" {
			return fmt.Errorf("existing release target requires exact retained identity")
		}
		if !isReleaseSHA256(intent.beforeSHA256) || intent.beforeMode == 0 || intent.beforeMode&^os.ModePerm != 0 {
			return fmt.Errorf("native release has invalid before-image facts")
		}
	} else if intent.beforeSHA256 != "" || intent.beforeMode != 0 {
		return fmt.Errorf("native release absent before-image carries file facts")
	}
	if intent.write {
		if !intent.beforeExists || !isReleaseSHA256(intent.afterSHA256) {
			return fmt.Errorf("native release writes require an existing file and an after-image digest")
		}
	} else if intent.afterSHA256 != "" {
		return fmt.Errorf("native release retained file cannot carry a publication digest")
	}
	if intent.role == NativeReleaseState && (!intent.write || intent.beforeSHA256 == intent.afterSHA256) {
		return fmt.Errorf("native release requires a distinct existing state-file commit")
	}
	return nil
}

func (intent NativeReleaseFileIntent) Role() NativeReleaseFileRole    { return intent.role }
func (intent NativeReleaseFileIntent) Target() NativeReleaseNamespace { return intent.target }
func (intent NativeReleaseFileIntent) BeforeExists() bool             { return intent.beforeExists }
func (intent NativeReleaseFileIntent) BeforeSHA256() string           { return intent.beforeSHA256 }
func (intent NativeReleaseFileIntent) BeforeMode() os.FileMode        { return intent.beforeMode }
func (intent NativeReleaseFileIntent) AfterSHA256() string            { return intent.afterSHA256 }
func (intent NativeReleaseFileIntent) Writes() bool                   { return intent.write }

func isReleaseSHA256(value string) bool {
	digest, prefixed := strings.CutPrefix(value, "sha256:")
	if !prefixed || len(digest) != sha256.Size*2 {
		return false
	}
	for _, digit := range digest {
		if !('0' <= digit && digit <= '9') && !('a' <= digit && digit <= 'f') {
			return false
		}
	}
	return true
}

func canonicalReleaseFiles(files []NativeReleaseFileIntent) ([]NativeReleaseFileIntent, error) {
	if len(files) != 3 {
		return nil, fmt.Errorf("native release requires manifest, default-lock and state file intents")
	}
	byRole := make(map[NativeReleaseFileRole]NativeReleaseFileIntent, len(files))
	for _, file := range files {
		if err := file.validate(); err != nil {
			return nil, err
		}
		if _, repeated := byRole[file.role]; repeated {
			return nil, fmt.Errorf("native release repeats a local metadata role")
		}
		byRole[file.role] = file
	}
	for index, file := range files {
		for _, prior := range files[:index] {
			if file.target.matches(prior.target) {
				return nil, fmt.Errorf("native release metadata roles repeat one target")
			}
		}
	}
	return []NativeReleaseFileIntent{byRole[NativeReleaseManifest], byRole[NativeReleaseDefaultLock], byRole[NativeReleaseState]}, nil
}
