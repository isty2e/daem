package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"runtime/debug"
	"testing"

	"github.com/isty2e/daem/internal/buildidentity"
)

const versionTestRevision = "0123456789abcdef0123456789abcdef01234567"

func TestPrintVersionWritesStableHumanLine(t *testing.T) {
	identity := versionTestIdentity(t)
	var output bytes.Buffer
	if err := PrintVersion(&output, identity); err != nil {
		t.Fatalf("PrintVersion returned error: %v", err)
	}
	want := "daem version=v1.2.3 revision=" + versionTestRevision + " source=clean go=go1.26.5 target=linux/amd64\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestPrintVersionJSONHasExactSchemaAndFacts(t *testing.T) {
	var output bytes.Buffer
	if err := PrintVersionJSON(&output, versionTestIdentity(t)); err != nil {
		t.Fatalf("PrintVersionJSON returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode version JSON: %v", err)
	}
	if len(payload) != 9 {
		t.Fatalf("JSON fields = %#v, want exactly 9", payload)
	}
	want := map[string]any{
		"schema_version": float64(1),
		"version":        "v1.2.3",
		"revision":       versionTestRevision,
		"revision_time":  "2026-07-01T02:03:04Z",
		"source_state":   "clean",
		"vcs":            "git",
		"go_version":     "go1.26.5",
		"goos":           "linux",
		"goarch":         "amd64",
	}
	for key, wantValue := range want {
		if payload[key] != wantValue {
			t.Fatalf("%s = %#v, want %#v", key, payload[key], wantValue)
		}
	}
}

func TestVersionPresentationKeepsUnknownFactsExplicit(t *testing.T) {
	var human bytes.Buffer
	if err := PrintVersion(&human, buildidentity.Identity{}); err != nil {
		t.Fatalf("PrintVersion returned error: %v", err)
	}
	wantHuman := "daem version=unknown revision=unknown source=unknown go=unknown target=unknown/unknown\n"
	if human.String() != wantHuman {
		t.Fatalf("human output = %q, want %q", human.String(), wantHuman)
	}

	var machine bytes.Buffer
	if err := PrintVersionJSON(&machine, buildidentity.Identity{}); err != nil {
		t.Fatalf("PrintVersionJSON returned error: %v", err)
	}
	for _, fact := range []string{`"version": "unknown"`, `"revision_time": "unknown"`, `"source_state": "unknown"`, `"goos": "unknown"`} {
		if !bytes.Contains(machine.Bytes(), []byte(fact)) {
			t.Fatalf("JSON = %s, want %s", machine.Bytes(), fact)
		}
	}
}

func TestVersionPresentationReturnsWriterErrors(t *testing.T) {
	want := errors.New("output closed")
	writer := presentErrorWriter{err: want}
	if err := PrintVersion(writer, versionTestIdentity(t)); !errors.Is(err, want) {
		t.Fatalf("PrintVersion error = %v, want %v", err, want)
	}
	if err := PrintVersionJSON(writer, versionTestIdentity(t)); !errors.Is(err, want) {
		t.Fatalf("PrintVersionJSON error = %v, want %v", err, want)
	}
}

type presentErrorWriter struct{ err error }

func (writer presentErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

func versionTestIdentity(t *testing.T) buildidentity.Identity {
	t.Helper()
	identity, err := buildidentity.FromBuildInfo(debug.BuildInfo{
		Path:      buildidentity.MainPackagePath,
		GoVersion: "go1.26.5",
		Main:      debug.Module{Path: buildidentity.MainModulePath, Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: versionTestRevision},
			{Key: "vcs.time", Value: "2026-07-01T02:03:04Z"},
			{Key: "vcs.modified", Value: "false"},
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
			{Key: "CGO_ENABLED", Value: "0"},
		},
	})
	if err != nil {
		t.Fatalf("build test identity: %v", err)
	}
	return identity
}
