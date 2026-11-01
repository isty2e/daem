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
	LegacyUserStateDir       string
	home                     homeSelection
}

type homeSelection struct {
	directory string
	cause     error
	selected  bool
}

func selectHome() homeSelection {
	directory, err := os.UserHomeDir()
	return homeSelection{directory: directory, cause: err, selected: true}
}

func (home homeSelection) resolve() (string, error) {
	if !home.selected {
		return "", fmt.Errorf("user home was not selected at path ingress")
	}
	if home.cause != nil {
		return "", fmt.Errorf("resolve user home directory: %w", home.cause)
	}
	return home.directory, nil
}

func (paths Paths) UserHomeDir() (string, error) { return paths.home.resolve() }

// Resolve expands an optional manifest path into daem's manifest, lock, state, cache, and recovery paths.
func Resolve(manifestPath string) (Paths, error) {
	home := selectHome()
	if manifestPath == "" {
		return implicitPaths(home)
	}
	return resolveExplicit(manifestPath, home)
}

// ResolveCreation selects an explicit manifest or ./daem.toml without falling back to user-global state.
func ResolveCreation(manifestPath string) (Paths, error) {
	home := selectHome()
	if manifestPath != "" {
		return resolveExplicit(manifestPath, home)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve working directory: %w", err)
	}
	return resolveExplicit(filepath.Join(workingDirectory, manifestFileName), home)
}

func resolveExplicit(manifestPath string, home homeSelection) (Paths, error) {
	absolutePath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve manifest path %q: %w", manifestPath, err)
	}

	manifestRoot := filepath.Dir(absolutePath)
	stateDir := filepath.Join(manifestRoot, localStateDirName)
	cacheDir := filepath.Join(stateDir, "cache")
	dataDir, err := defaultRootDataDir(home)
	if err != nil {
		return Paths{}, err
	}

	return selectUserStorage(buildPaths(absolutePath, manifestRoot, ManifestOriginExplicit, stateDir, cacheDir, dataDir, home))
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

func implicitPaths(home homeSelection) (Paths, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve working directory: %w", err)
	}
	cwdManifestPath := filepath.Join(workingDirectory, manifestFileName)
	if _, err := os.Stat(cwdManifestPath); err == nil {
		stateDir := filepath.Join(workingDirectory, localStateDirName)
		cacheDir := filepath.Join(stateDir, "cache")
		dataDir, err := defaultRootDataDir(home)
		if err != nil {
			return Paths{}, err
		}
		return selectUserStorage(buildPaths(cwdManifestPath, workingDirectory, ManifestOriginCWD, stateDir, cacheDir, dataDir, home))
	} else if !os.IsNotExist(err) {
		return Paths{}, fmt.Errorf("inspect cwd manifest path %q: %w", cwdManifestPath, err)
	}

	return defaultPaths(home)
}

func defaultPaths(home homeSelection) (Paths, error) {
	configDir, err := defaultRootConfigDir(home)
	if err != nil {
		return Paths{}, err
	}
	stateDir, err := defaultRootStateDir(home)
	if err != nil {
		return Paths{}, err
	}
	cacheDir, err := defaultRootCacheDir(home)
	if err != nil {
		return Paths{}, err
	}
	dataDir, err := defaultRootDataDir(home)
	if err != nil {
		return Paths{}, err
	}

	manifestPath := filepath.Join(configDir, manifestFileName)
	paths := buildPaths(manifestPath, configDir, ManifestOriginUserDefault, stateDir, cacheDir, dataDir, home)
	paths.LegacyUserStateDir = filepath.Join(configDir, localStateDirName)
	if paths.LegacyUserStateDir == paths.StateDir {
		paths.LegacyUserStateDir = ""
	}
	return paths, nil
}

func buildPaths(manifestPath string, manifestRoot string, origin ManifestOrigin, stateDir string, cacheDir string, dataDir string, home homeSelection) Paths {
	return Paths{
		home:                     home,
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

func defaultRootConfigDir(home homeSelection) (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName), nil
	}

	return defaultUnixRootDir(home, "XDG_CONFIG_HOME", ".config")
}

func defaultRootStateDir(home homeSelection) (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve local application data directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "state"), nil
	}

	return defaultUnixRootDir(home, "XDG_STATE_HOME", ".local", "state")
}

func defaultRootCacheDir(home homeSelection) (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve user cache directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "cache"), nil
	}

	return defaultUnixRootDir(home, "XDG_CACHE_HOME", ".cache")
}

func defaultRootDataDir(home homeSelection) (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve local application data directory: %w", err)
		}
		return filepath.Join(root, appDirectoryName, "data"), nil
	}

	return defaultUnixRootDir(home, "XDG_DATA_HOME", ".local", "share")
}

func defaultUnixRootDir(home homeSelection, envName string, fallbackSegments ...string) (string, error) {
	if root := os.Getenv(envName); root != "" {
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("%s must be an absolute path: %q", envName, root)
		}
		return filepath.Join(root, appDirectoryName), nil
	}

	homeDir, err := home.resolve()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}

	segments := append([]string{homeDir}, fallbackSegments...)
	segments = append(segments, appDirectoryName)
	return filepath.Join(segments...), nil
}
