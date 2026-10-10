package host

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeConfiguredAdapterRefusesWithoutInventoryOrLocationQueries(t *testing.T) {
	for _, entry := range []string{
		`"npm:pi-mcp-adapter@2.15.0"`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","autoload":false,"extensions":[]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","autoload":false,"extensions":["-index.ts"]}`,
	} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			t.Run(entry+"/"+string(scope), func(t *testing.T) {
				workDir, agentRoot := t.TempDir(), t.TempDir()
				log := installNativeQueryCanary(t)
				base := agentRoot
				if scope == target.ScopeProject {
					base = filepath.Join(workDir, ".pi")
				}
				writeEffectiveConfig(t, filepath.Join(base, "settings.json"), `{"npmCommand":["npm","install"],"packages":[`+entry+`]}`)
				contract, err := profile.PiMCPContractForBackend(mcp.BackendNative)
				if err != nil {
					t.Fatal(err)
				}
				err = qualifyPiNativeSettings(t.Context(), contract, target.ScopeProject, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
				if !errors.Is(err, profile.ErrPiNativeAdapterConfigured) {
					t.Fatalf("configured intent = %v", err)
				}
				assertNativeQueryNotCalled(t, log)
			})
		}
	}
}

func installNativeQueryCanary(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "queries.txt")
	t.Setenv("PATH", bin)
	t.Setenv("DAEM_NATIVE_QUERY_LOG", log)
	for _, name := range []string{"npm", "pnpm", "bun"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DAEM_NATIVE_QUERY_LOG\"\nexit 97\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 91\nprintf '1.0.2\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return log
}

func assertNativeQueryNotCalled(t *testing.T, log string) {
	t.Helper()
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("unexpected host package query: %v", err)
	}
}
