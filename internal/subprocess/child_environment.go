package subprocess

import (
	"maps"
	"os"
	"slices"
	"strings"
)

// ChildEnvironmentInheritancePolicy is the stable disclosure value for child environments.
const ChildEnvironmentInheritancePolicy = "inherit"

// ChildEnvironmentSnapshot is one immutable child-process environment plus explicit secret
// overrides. It is execution input, not portable identity or durable evidence.
type ChildEnvironmentSnapshot struct {
	entries         []string
	explicitSecrets map[string]string
}

// InheritedChildEnvironment captures the current process environment for one child launch.
func InheritedChildEnvironment() ChildEnvironmentSnapshot {
	return ChildEnvironmentFrom(os.Environ())
}

// ChildEnvironmentFrom constructs a snapshot from environment entries.
func ChildEnvironmentFrom(entries []string) ChildEnvironmentSnapshot {
	return ChildEnvironmentSnapshot{
		entries:         append([]string(nil), entries...),
		explicitSecrets: make(map[string]string),
	}
}

// WithSecret returns a snapshot where name has value and value is always a
// redaction candidate, even when name is not conventionally secret-shaped.
func (snapshot ChildEnvironmentSnapshot) WithSecret(name string, value string) ChildEnvironmentSnapshot {
	entries := make([]string, 0, len(snapshot.entries)+1)
	prefix := name + "="
	for _, entry := range snapshot.entries {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		entries = append(entries, entry)
	}
	entries = append(entries, prefix+value)

	explicit := make(map[string]string, len(snapshot.explicitSecrets)+1)
	maps.Copy(explicit, snapshot.explicitSecrets)
	explicit[name] = value
	return ChildEnvironmentSnapshot{entries: entries, explicitSecrets: explicit}
}

// Entries returns a detached environment suitable for exec.Cmd.Env.
func (snapshot ChildEnvironmentSnapshot) Entries() []string {
	return append([]string(nil), snapshot.entries...)
}

// SecretValues returns non-empty explicit values and values whose final child
// environment names conventionally carry credentials or session secrets.
func (snapshot ChildEnvironmentSnapshot) SecretValues() []string {
	seen := make(map[string]struct{})
	values := make([]string, 0, len(snapshot.explicitSecrets))
	appendValue := func(value string) {
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	for _, entry := range snapshot.entries {
		name, value, ok := strings.Cut(entry, "=")
		if ok && isSensitiveName(name) {
			appendValue(value)
		}
	}
	for _, value := range snapshot.explicitSecrets {
		appendValue(value)
	}
	slices.Sort(values)
	return values
}

func isSensitiveName(name string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "DATABASE_URL", "REDIS_URL", "MONGODB_URI", "AMQP_URL", "BROKER_URL",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "DOCKER_AUTH_CONFIG",
		"PGPASSWORD", "MYSQL_PWD":
		return true
	}
	for _, segment := range []string{
		"TOKEN",
		"SECRET",
		"PASSWORD",
		"PASSWD",
		"PASS",
		"API_KEY",
		"APIKEY",
		"ACCESS_KEY",
		"ACCESSKEY",
		"PRIVATE_KEY",
		"PRIVATEKEY",
		"CREDENTIAL",
		"CREDENTIALS",
		"COOKIE",
		"COOKIES",
		"CONNECTION_STRING",
		"AUTHORIZATION",
		"AUTH",
		"JWT",
		"DSN",
	} {
		if hasNameSegment(normalized, segment) {
			return true
		}
	}
	return false
}

func hasNameSegment(name string, segment string) bool {
	return name == segment ||
		strings.HasPrefix(name, segment+"_") ||
		strings.HasSuffix(name, "_"+segment) ||
		strings.Contains(name, "_"+segment+"_")
}
