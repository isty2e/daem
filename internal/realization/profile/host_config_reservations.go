package profile

import (
	"strings"

	"github.com/isty2e/daem/internal/target"
)

type hostConfigPathReservation struct {
	path       string
	owner      string
	globalOnly bool
}

var piHostConfigPathReservations = []hostConfigPathReservation{
	{"auth.json", "authentication", true},
	{"auth.json.lock", "authentication locking", true},
	{"settings.json.lock", "host settings locking", false},
	{"models-store.json", "the refreshed model cache", true},
	{"models-store.json.lock", "model cache locking", true},
	{"sessions", "session state", true},
	{"crashes.json", "crash diagnostics", true},
	{"pi-debug.log", "runtime diagnostics", true},
	{"npm", "package installation", false},
	{"git", "package installation", false},
	{"tmp", "temporary runtime artifacts", true},
	{"bin", "installed helper code", true},
	{"tools", "host tool artifacts", true},
}

var codexHostConfigPathReservations = []hostConfigPathReservation{
	{"auth.json", "authentication", true},
	{".credentials.json", "MCP authentication", true},
	{"secrets", "encrypted credentials", true},
	{"mcp-oauth-locks", "MCP authentication locking", true},
	{"sessions", "session state", true},
	{"archived_sessions", "session state", true},
	{"history.jsonl", "message history", true},
	{"session_index.jsonl", "the session index", true},
	{"session_index.jsonl.tmp", "session index publication", true},
	{"shell_snapshots", "runtime shell snapshots", true},
	{"memories", "generated memory state", true},
	{"installation_id", "runtime identity", true},
	{"db-backups", "database recovery", true},
	{"log", "runtime logs", true},
	{"models_cache.json", "the model cache", true},
	{"version.json", "update state", true},
	{"cloud-config-bundle-cache.json", "the cloud configuration cache", true},
	{"cache/remote_plugin_catalog", "the plugin catalog cache", true},
	{"cache/codex_app_directory", "the connector directory cache", true},
	{"plugins/cache", "installed plugin code", true},
	{".tmp/marketplaces", "installed marketplaces", true},
	{".tmp/plugin-share-local-paths-v1.json", "plugin sharing state", true},
	{".tmp/rollout-compression.lock", "session compression locking", true},
	{"tmp/arg0", "runtime helper code", true},
	{"packages/standalone", "the standalone installation", true},
	{"app-server-control", "app-server control", true},
	{"app-server-daemon/daemon.lock", "daemon locking", true},
	{".sandbox", "sandbox state", true},
	{".sandbox-bin", "sandbox helper code", true},
	{".sandbox-secrets", "sandbox credentials", true},
}

func hostConfigPathOwner(selected target.Target, scope target.Scope, nativePath string) (string, bool) {
	if hostConfigPathsOverlap(nativePath, ".git") {
		return "repository metadata", true
	}
	reservations := piHostConfigPathReservations
	if selected == target.TargetCodex {
		reservations = codexHostConfigPathReservations
	}
	for _, reservation := range reservations {
		if reservation.globalOnly && scope != target.ScopeGlobal {
			continue
		}
		if hostConfigPathsOverlap(nativePath, reservation.path) {
			return reservation.owner, true
		}
	}
	if selected == target.TargetPi && hostConfigPiLoaderPath(nativePath) {
		return "extension loading", true
	}
	if selected == target.TargetCodex && scope == target.ScopeGlobal && hostConfigCodexRuntimePath(nativePath) {
		return "runtime state", true
	}
	return "", false
}

func hostConfigPathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func hostConfigPiLoaderPath(nativePath string) bool {
	parts := strings.Split(nativePath, "/")
	if parts[0] != "extensions" {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	name := parts[1]
	if name == "index.ts" || name == "index.js" || name == "package.json" ||
		name == ".gitignore" || name == ".ignore" || name == ".fdignore" {
		return true
	}
	if len(parts) == 2 {
		return strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js")
	}
	return parts[2] == "index.ts" || parts[2] == "index.js" || parts[2] == "package.json"
}

func hostConfigCodexRuntimePath(nativePath string) bool {
	parts := strings.Split(nativePath, "/")
	name := parts[0]
	if strings.HasSuffix(name, "-wal") {
		name = strings.TrimSuffix(name, "-wal")
	} else {
		name = strings.TrimSuffix(name, "-shm")
	}
	for _, prefix := range []string{"state_", "logs_", "goals_", "memories_", "thread_history_"} {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".sqlite")
		if version != "" && strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			return true
		}
	}
	if len(parts) < 2 || parts[0] != "app-server-daemon" {
		return false
	}
	for _, stem := range []string{"app-server", "app-server-updater"} {
		for _, suffix := range []string{".pid", ".pid.lock", ".pid.tmp", ".stderr.log"} {
			if parts[1] == stem+suffix {
				return true
			}
		}
	}
	return false
}
