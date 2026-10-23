package profile

import (
	"strings"

	"github.com/isty2e/daem/internal/target"
)

type hostConfigPathReservation struct {
	path  string
	owner string
}

var piHostConfigPathReservations = []hostConfigPathReservation{
	{"AGENTS.md", "instructions"},
	{"skills", "skills"},
	{"mcp.json", "MCP servers"},
	{"auth.json", "authentication"},
	{"auth.json.lock", "authentication locking"},
	{"settings.json.lock", "host settings locking"},
	{"models-store.json", "the refreshed model cache"},
	{"models-store.json.lock", "model cache locking"},
	{"sessions", "session state"},
	{"crashes.json", "crash diagnostics"},
	{"pi-debug.log", "runtime diagnostics"},
	{"npm", "package installation"},
	{"git", "package installation"},
	{"tmp", "temporary runtime artifacts"},
	{"bin", "installed helper code"},
	{"tools", "host tool artifacts"},
}

var codexHostConfigPathReservations = []hostConfigPathReservation{
	{"AGENTS.md", "instructions"},
	{"AGENTS.override.md", "instructions"},
	{"skills", "skills"},
	{"hooks.json", "hooks"},
	{"auth.json", "authentication"},
	{".credentials.json", "MCP authentication"},
	{"secrets", "encrypted credentials"},
	{"mcp-oauth-locks", "MCP authentication locking"},
	{"sessions", "session state"},
	{"archived_sessions", "session state"},
	{"history.jsonl", "message history"},
	{"session_index.jsonl", "the session index"},
	{"session_index.jsonl.tmp", "session index publication"},
	{"shell_snapshots", "runtime shell snapshots"},
	{"memories", "generated memory state"},
	{"installation_id", "runtime identity"},
	{"db-backups", "database recovery"},
	{"log", "runtime logs"},
	{"models_cache.json", "the model cache"},
	{"version.json", "update state"},
	{"cloud-config-bundle-cache.json", "the cloud configuration cache"},
	{"cache/remote_plugin_catalog", "the plugin catalog cache"},
	{"cache/codex_app_directory", "the connector directory cache"},
	{"plugins/cache", "installed plugin code"},
	{"plugins/data", "plugin runtime data"},
	{".tmp/marketplaces", "installed marketplaces"},
	{".tmp/plugin-share-local-paths-v1.json", "plugin sharing state"},
	{".tmp/rollout-compression.lock", "session compression locking"},
	{"tmp/arg0", "runtime helper code"},
	{"packages/standalone", "the standalone installation"},
	{"app-server-control", "app-server control"},
	{"app-server-daemon/daemon.lock", "daemon locking"},
	{".sandbox", "sandbox state"},
	{".sandbox-bin", "sandbox helper code"},
	{".sandbox-secrets", "sandbox credentials"},
}

func hostConfigPathOwner(selected target.Target, scope target.Scope, nativePath string) (string, bool) {
	if hostConfigPathsOverlap(nativePath, ".git") {
		return "repository metadata", true
	}
	parts := strings.Split(nativePath, "/")
	if len(parts) > 1 && hostConfigDocumentClass(selected, scope, parts[0]) != hostConfigOrdinaryDocument {
		return "the host configuration document", true
	}
	reservations := piHostConfigPathReservations
	if selected == target.TargetCodex {
		reservations = codexHostConfigPathReservations
	}
	for _, reservation := range reservations {
		if hostConfigPathsOverlap(nativePath, reservation.path) {
			return reservation.owner, true
		}
	}
	if selected == target.TargetPi && hostConfigPiLoaderPath(nativePath) {
		return "extension loading", true
	}
	if selected == target.TargetCodex && hostConfigCodexRuntimePath(nativePath) {
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
	if len(parts) == 2 {
		name := parts[1]
		return strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js") ||
			name == "package.json" || name == ".gitignore" || name == ".ignore" || name == ".fdignore"
	}
	return len(parts) == 3 && (parts[2] == "index.ts" || parts[2] == "index.js" || parts[2] == "package.json")
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
