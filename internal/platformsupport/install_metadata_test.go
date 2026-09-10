package platformsupport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCommitMetadataSelectsPinnedCommitterTime(t *testing.T) {
	const revision = "2bf957187f9f847aa87b0e807d6ca960589f1083"
	const timestamp = "2026-07-28T02:19:30Z"
	valid := `{"sha":"` + revision + `","committer":{"date":"` + timestamp + `"}}`
	tests := []struct {
		name     string
		document string
		accepted bool
	}{
		{"published release", valid, true},
		{"metadata byte boundary", valid + strings.Repeat(" ", 65536-len(valid)), true},
		{"oversized metadata", valid + strings.Repeat(" ", 65537-len(valid)), false},
		{"reordered and unrelated fields", `{"message":"quoted \"date\" and \\ paths\n\uAC00","parents":[{"sha":"other"}],"author":{"date":"2000-01-01T00:00:00Z"},"committer":{"name":"author","date":"` + timestamp + `"},"verified":false,"extra":null,"number":-1.25e3,"sha":"` + revision + `"}`, true},
		{"multiline", "{\n\"sha\":\"" + revision + "\",\n\"committer\": {\"date\":\"" + timestamp + "\"}\n}\n", true},
		{"wrong root sha", strings.Replace(valid, revision, strings.Repeat("a", 40), 1), false},
		{"nested sha only", `{"tree":{"sha":"` + revision + `"},"committer":{"date":"` + timestamp + `"}}`, false},
		{"author date only", `{"sha":"` + revision + `","author":{"date":"` + timestamp + `"}}`, false},
		{"dotted key is not path", `{"sha":"` + revision + `","committer.date":"` + timestamp + `"}`, false},
		{"array is not committer", `{"sha":"` + revision + `","committer":[{"date":"` + timestamp + `"}]}`, false},
		{"duplicate sha", strings.Replace(valid, `"sha":`, `"sha":"other","sha":`, 1), false},
		{"duplicate committer", strings.TrimSuffix(valid, "}") + `,"committer":{"date":"2000-01-01T00:00:00Z"}}`, false},
		{"duplicate date", strings.Replace(valid, `"date":`, `"date":"2000-01-01T00:00:00Z","date":`, 1), false},
		{"escaped key", strings.Replace(valid, `"sha"`, `"sh\u0061"`, 1), false},
		{"escaped identity", strings.Replace(valid, `"2bf`, `"\u0032bf`, 1), false},
		{"missing closing brace", strings.TrimSuffix(valid, "}"), false},
		{"trailing document", valid + valid, false},
		{"trailing comma", strings.TrimSuffix(valid, "}") + ",}", false},
		{"bad number", strings.TrimSuffix(valid, "}") + `,"number":01}`, false},
		{"bad escape", strings.TrimSuffix(valid, "}") + `,"message":"\q"}`, false},
		{"bad unicode escape", strings.TrimSuffix(valid, "}") + `,"message":"\uZZZZ"}`, false},
		{"invalid timestamp", strings.Replace(valid, timestamp, "2026-99-99T02:19:30Z", 1), false},
		{"null timestamp", strings.Replace(valid, `"`+timestamp+`"`, "null", 1), false},
		{"bounded nesting", strings.TrimSuffix(valid, "}") + `,"extra":` + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34) + "}", false},
	}
	functions := installRecipeFunctions(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "commit.json")
			if err := os.WriteFile(path, []byte(test.document), 0o600); err != nil {
				t.Fatal(err)
			}
			invocation := `actual="$(daem_commit_revision_time "$1" "$2")" || exit 1; test "$actual" = "$3"`
			if !test.accepted {
				invocation = `if daem_commit_revision_time "$1" "$2" >/dev/null; then exit 1; fi`
			}
			if err := runInstallShell(functions, invocation, nil, path, revision, timestamp); err != nil {
				t.Fatalf("metadata acceptance/output contract failed: %v, want accepted=%t", err, test.accepted)
			}
		})
	}
}

func TestInstallReadsPinnedToolchainDirective(t *testing.T) {
	tests := []struct {
		module    string
		toolchain string
	}{
		{"module github.com/isty2e/daem\ngo 1.25.0\ntoolchain go1.26.5\n", "go1.26.5"},
		{"// toolchain go1.1.1\n\ttoolchain   go1.26.6 // selected\n", "go1.26.6"},
		{"module github.com/isty2e/daem\ngo 1.25.0\n", ""},
		{"toolchain go1.26.5\ntoolchain go1.26.5\n", ""},
		{"toolchain go1.26.5 extra\n", ""},
		{"toolchain default\n", ""},
		{"toolchain go1.26rc1\n", ""},
		{"toolchain go01.26.5\n", ""},
		{"toolchain\n", ""},
		{strings.Repeat(" ", 65536) + "toolchain go1.26.5\n", ""},
	}
	functions := installRecipeFunctions(t)
	for index, test := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.mod")
			if err := os.WriteFile(path, []byte(test.module), 0o600); err != nil {
				t.Fatal(err)
			}
			invocation := `actual="$(daem_release_toolchain "$1")" || exit 1; test "$actual" = "$2"`
			if test.toolchain == "" {
				invocation = `if daem_release_toolchain "$1" >/dev/null; then exit 1; fi`
			}
			if err := runInstallShell(functions, invocation, nil, path, test.toolchain); err != nil {
				t.Fatalf("toolchain acceptance/output contract failed: %v, want %q", err, test.toolchain)
			}
		})
	}
}
