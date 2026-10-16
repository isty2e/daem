package manifest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestSelectedInputErrorCausePreservesInputFailures(t *testing.T) {
	for _, input := range []struct {
		name    string
		body    string
		missing bool
		user    bool
	}{
		{name: "read", missing: true},
		{name: "syntax", body: "version=[\n"},
		{name: "validation", body: "version=1\ntargets=[\"codex\"]\nbogus=true\n"},
		{name: "placement", body: "version=1\ntargets=[\"codex\"]\n[instructions.project]\nsource={path=\"instructions\",mode=\"vendor\"}\n", user: true},
	} {
		t.Run(input.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			t.Setenv("APPDATA", filepath.Join(root, "appdata"))
			t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
			manifestPath := filepath.Join(root, "daem.toml")
			if !input.missing {
				if err := os.WriteFile(manifestPath, []byte(input.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			paths, err := daempaths.Resolve(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if input.user {
				paths.ManifestOrigin = daempaths.ManifestOriginUserDefault
			}
			_, err = LoadSelected(t.Context(), paths)
			cause, marked := InputErrorCause(err)
			if err == nil || !marked || cause == nil {
				t.Fatal("selected read/validation failure lost input provenance")
			}
			if err.Error() != cause.Error() || !errors.Is(err, cause) {
				t.Error("input provenance changed error text or cause identity")
			}
			if _, marked := InputErrorCause(fmt.Errorf("outer: %w", err)); !marked {
				t.Error("input provenance was lost under wrapping")
			}
			if input.missing && !errors.Is(err, os.ErrNotExist) {
				t.Error("input provenance lost the file-read classification")
			}
			if input.name == "validation" {
				var unknown declaration.UnknownManifestKeyError
				if !errors.As(err, &unknown) {
					t.Error("input provenance lost typed validation evidence")
				}
			}
		})
	}
}

func TestSelectedInputErrorCauseDoesNotGuessFromText(t *testing.T) {
	for _, err := range []error{nil, errors.New("invalid manifest: missing key"), errors.New("resolve carrier local source home")} {
		if _, marked := InputErrorCause(err); marked {
			t.Fatal("an unowned lookalike error gained input provenance")
		}
	}

	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version=1\ntargets=[\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = LoadSelected(ctx, paths)
	if !errors.Is(err, context.Canceled) {
		t.Error("input loading lost cancellation precedence evidence")
	}
}
