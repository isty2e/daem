package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	appDirectoryName  = "daem"
	manifestFileName  = "daem.toml"
	lockfileFileName  = "daem.lock.toml"
	localStateDirName = ".daem"
)

const (
	// AppDirectoryName is the default daem directory name under OS config, state, and cache roots.
	AppDirectoryName = appDirectoryName
	// ManifestFileName is the default manifest filename.
	ManifestFileName = manifestFileName
)

// ManifestOrigin identifies how the selected manifest path was chosen.
type ManifestOrigin string

const (
	// ManifestOriginExplicit means the operator provided --manifest.
	ManifestOriginExplicit ManifestOrigin = "explicit"
	// ManifestOriginCWD means --manifest was omitted and ./daem.toml existed in the command cwd.
	ManifestOriginCWD ManifestOrigin = "cwd"
	// ManifestOriginUserDefault means --manifest was omitted and no cwd manifest existed.
	ManifestOriginUserDefault ManifestOrigin = "user-default"
)

// Paths contains the resolved filesystem locations daem commands use.
type Paths struct {
	ManifestPath             string
	ManifestRoot             string
	ManifestOrigin           ManifestOrigin
	LockfilePath             string
	StateDir                 string
	StatefilePath            string
	CacheDir                 string
	DataDir                  string
	OwnershipRegistryPath    string
	CarrierClaimRegistryPath string
	SourceCacheDir           string
	RecoveryDir              string
}

// Resolve expands an optional manifest path into daem's manifest, lock, state, cache, and recovery paths.
func Resolve(manifestPath string) (Paths, error) {
	if manifestPath == "" {
		return implicitPaths()
	}
	return resolveExplicit(manifestPath)
}

// ResolveCreation selects an explicit manifest or ./daem.toml without falling back to user-global state.
func ResolveCreation(manifestPath string) (Paths, error) {
	if manifestPath != "" {
		return resolveExplicit(manifestPath)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve working directory: %w", err)
	}
	return resolveExplicit(filepath.Join(workingDirectory, manifestFileName))
}

func resolveExplicit(manifestPath string) (Paths, error) {
	absolutePath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve manifest path %q: %w", manifestPath, err)
	}

	manifestRoot := filepath.Dir(absolutePath)
	stateDir := filepath.Join(manifestRoot, localStateDirName)
	cacheDir := filepath.Join(stateDir, "cache")
	dataDir, err := defaultRootDataDir()
	if err != nil {
		return Paths{}, err
	}

	return buildPaths(absolutePath, manifestRoot, ManifestOriginExplicit, stateDir, cacheDir, dataDir), nil
}

// ProjectPlacementAllowed reports whether project-scoped target-visible writes may use ManifestRoot.
func (paths Paths) ProjectPlacementAllowed() bool {
	return paths.ManifestOrigin == ManifestOriginExplicit || paths.ManifestOrigin == ManifestOriginCWD
}

// WithDataDir returns a correlated copy rooted at one already-selected
// physical data directory.
func (paths Paths) WithDataDir(dataDir string) (Paths, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return Paths{}, fmt.Errorf("data directory %q must be absolute and clean", dataDir)
	}
	updated := paths
	updated.DataDir = dataDir
	updated.OwnershipRegistryPath = filepath.Join(dataDir, "ownership", "claims.json")
	updated.CarrierClaimRegistryPath = filepath.Join(dataDir, "carriers", "claims.json")
	return updated, nil
}

func implicitPaths() (Paths, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve working directory: %w", err)
	}
	cwdManifestPath := filepath.Join(workingDirectory, manifestFileName)
	if _, err := os.Stat(cwdManifestPath); err == nil {
		stateDir := filepath.Join(workingDirectory, localStateDirName)
		cacheDir := filepath.Join(stateDir, "cache")
		dataDir, err := defaultRootDataDir()
		if err != nil {
			return Paths{}, err
		}
		return buildPaths(cwdManifestPath, workingDirectory, ManifestOriginCWD, stateDir, cacheDir, dataDir), nil
	} else if !os.IsNotExist(err) {
		return Paths{}, fmt.Errorf("inspect cwd manifest path %q: %w", cwdManifestPath, err)
	}

	return defaultPaths()
}

func defaultPaths() (Paths, error) {
	configDir, err := defaultRootConfigDir()
	if err != nil {
		return Paths{}, err
	}
	stateDir, err := defaultRootStateDir()
	if err != nil {
		return Paths{}, err
	}
	cacheDir, err := defaultRootCacheDir()
	if err != nil {
		return Paths{}, err
	}
	dataDir, err := defaultRootDataDir()
	if err != nil {
		return Paths{}, err
	}

	manifestPath := filepath.Join(configDir, manifestFileName)
	return buildPaths(manifestPath, configDir, ManifestOriginUserDefault, stateDir, cacheDir, dataDir), nil
}

func buildPaths(manifestPath string, manifestRoot string, origin ManifestOrigin, stateDir string, cacheDir string, dataDir string) Paths {
	return Paths{
		ManifestPath:             manifestPath,
		ManifestRoot:             manifestRoot,
		ManifestOrigin:           origin,
		LockfilePath:             filepath.Join(manifestRoot, lockfileFileName),
		StateDir:                 stateDir,
		StatefilePath:            filepath.Join(stateDir, "state.json"),
		CacheDir:                 cacheDir,
		DataDir:                  dataDir,
		OwnershipRegistryPath:    filepath.Join(dataDir, "ownership", "claims.json"),
		CarrierClaimRegistryPath: filepath.Join(dataDir, "carriers", "claims.json"),
		SourceCacheDir:           filepath.Join(cacheDir, "sources"),
		RecoveryDir:              filepath.Join(stateDir, "recovery"),
	}
}

func defaultRootConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName), nil
	}

	return defaultUnixRootDir("XDG_CONFIG_HOME", ".config")
}

func defaultRootStateDir() (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve local application data directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "state"), nil
	}

	return defaultUnixRootDir("XDG_STATE_HOME", ".local", "state")
}

func defaultRootCacheDir() (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve user cache directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "cache"), nil
	}

	return defaultUnixRootDir("XDG_CACHE_HOME", ".cache")
}

func defaultRootDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve local application data directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "data"), nil
	}

	return defaultUnixRootDir("XDG_DATA_HOME", ".local", "share")
}

func defaultUnixRootDir(envName string, fallbackSegments ...string) (string, error) {
	if root := os.Getenv(envName); root != "" {
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("%s must be an absolute path: %q", envName, root)
		}
		return filepath.Join(root, appDirectoryName), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}

	segments := append([]string{homeDir}, fallbackSegments...)
	segments = append(segments, appDirectoryName)
	return filepath.Join(segments...), nil
}
