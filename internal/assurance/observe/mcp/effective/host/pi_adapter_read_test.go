package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/filesnapshot"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func adapterReadFixture(t *testing.T) PiAdapterInput {
	t.Helper()
	root := t.TempDir()
	return PiAdapterInput{
		Context: t.Context(), Projection: piEffectiveProjection(t, target.ScopeProject), Codecs: aggregatecodec.Catalog(),
		HomeDir: filepath.Join(root, "home"), AgentRoot: filepath.Join(root, "agent"), WorkDir: filepath.Join(root, "work"), SelectedPath: filepath.Join(root, "work", ".pi", "mcp.json"),
	}
}

func adapterReadMap(t *testing.T, documents map[string]string) func(context.Context, string, int64) ([]byte, bool, error) {
	t.Helper()
	return func(_ context.Context, path string, maximumBytes int64) ([]byte, bool, error) {
		if maximumBytes != maximumConfigBytes {
			t.Fatalf("adapter read lost its existing byte cap: %d", maximumBytes)
		}
		content, exists := documents[path]
		return []byte(content), exists, nil
	}
}

func TestObservePiAdapterRetainsNormalContentAndAbsence(t *testing.T) {
	input := adapterReadFixture(t)
	writeEffectiveConfig(t, input.SelectedPath, `{"mcpServers":{"context7":{"command":"decoy"}}}`)
	writeEffectiveConfig(t, filepath.Join(input.AgentRoot, "mcp.json"), `{"mcpServers":{"context7":{"command":"decoy"}}}`)
	input.ReadFile = adapterReadMap(t, map[string]string{input.SelectedPath: `{"mcpServers":{"context7":{"command":"node"}}}`})
	observed := mustObservePiAdapter(t, input)
	if observed.State() != mcpeffective.StateExact || len(observed.Sources()) != 6 {
		t.Fatal("adapter bypassed retained normal content or authoritative absence", observed.State())
	}
}

func TestObservePiAdapterRoutesAllImportsAndDiscoveryThroughReader(t *testing.T) {
	for _, discovery := range []bool{false, true} {
		for _, kind := range orderedImportKinds {
			t.Run(fmt.Sprintf("%s/discovery=%t", kind, discovery), func(t *testing.T) {
				input := adapterReadFixture(t)
				control := fmt.Sprintf(`{"imports":[%q],"mcpServers":{"context7":{"command":"node"}}}`, kind)
				if discovery {
					control = `{"settings":{"hostConfigDiscovery":"on"},"mcpServers":{"context7":{"command":"node"}}}`
				}
				path, content := "", `{"mcpServers":{"context7":{"command":"foreign"}}}`
				if kind == importOpenCode {
					path, content = filepath.Join(input.WorkDir, "opencode.json"), `{"mcp":{"context7":{"type":"local","command":["foreign"]}}}`
				} else {
					path = importCandidates(kind, input.HomeDir, input.WorkDir)[0]
					if kind == importCodex {
						content = "[mcp_servers.context7]\ncommand='foreign'\n"
					}
				}
				input.ReadFile = adapterReadMap(t, map[string]string{input.SelectedPath: control, path: content})
				observed := mustObservePiAdapter(t, input)
				found := false
				for _, source := range observed.Sources() {
					if source.Path() == path && source.DefinesSelectedName() {
						found = source.State() == mcpeffective.SourceExact && source.Precedence() == mcpeffective.PrecedenceLower
					}
				}
				if !found || observed.State() != mcpeffective.StateConflicting {
					t.Fatal("import/discovery bypassed the shared reader", observed.State())
				}
			})
		}
	}
}

func TestObservePiAdapterRetainsCodexFallbackAndOpaquePrecedence(t *testing.T) {
	for _, malformedPrimary := range []bool{false, true} {
		t.Run(fmt.Sprint(malformedPrimary), func(t *testing.T) {
			input := adapterReadFixture(t)
			primary, fallback := importCandidates(importCodex, input.HomeDir, input.WorkDir)[0], importCandidates(importCodex, input.HomeDir, input.WorkDir)[1]
			writeEffectiveConfig(t, primary, "[mcp_servers.decoy]\ncommand='wrong'\n")
			documents := map[string]string{input.SelectedPath: `{"imports":["codex"],"mcpServers":{"context7":{"command":"node"}}}`, fallback: `{"mcp_servers":{"context7":{"command":"foreign"}}}`}
			if malformedPrimary {
				documents[primary] = "[invalid"
			}
			input.ReadFile = adapterReadMap(t, documents)
			observed := mustObservePiAdapter(t, input)
			if malformedPrimary {
				if observed.State() != mcpeffective.StateUnobservable {
					t.Fatal("malformed primary fell through", observed.State())
				}
			} else if observed.State() != mcpeffective.StateConflicting {
				t.Fatal("retained-absent TOML did not allow JSON fallback", observed.State())
			}
		})
	}
}

func TestObservePiAdapterOpenCodePresenceUsesTheSelectedRead(t *testing.T) {
	for _, nestedExists := range []bool{false, true} {
		t.Run(fmt.Sprint(nestedExists), func(t *testing.T) {
			input := adapterReadFixture(t)
			gitRoot := input.WorkDir
			if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			input.WorkDir = filepath.Join(gitRoot, "nested")
			input.SelectedPath = filepath.Join(input.WorkDir, ".pi", "mcp.json")
			rootPath, nestedPath := filepath.Join(gitRoot, "opencode.json"), filepath.Join(input.WorkDir, "opencode.json")
			if !nestedExists {
				writeEffectiveConfig(t, nestedPath, `{"mcp":{"decoy":{"command":["wrong"]}}}`)
			}
			documents := map[string]string{
				input.SelectedPath: `{"imports":["opencode"],"mcpServers":{"context7":{"command":"node"}}}`,
				rootPath:           `{"mcp":{"context7":{"type":"local","command":["ancestor"]}}}`,
				filepath.Join(input.HomeDir, ".config", "opencode", "opencode.json"): `{"mcp":{"context7":{"type":"local","command":["global"]}}}`,
			}
			if nestedExists {
				documents[nestedPath] = `{"mcp":{"context7":{"args":["nested"]}}}`
			}
			reads := make(map[string]int)
			reader := adapterReadMap(t, documents)
			input.ReadFile = func(ctx context.Context, path string, maximumBytes int64) ([]byte, bool, error) {
				reads[path]++
				return reader(ctx, path, maximumBytes)
			}
			observed := mustObservePiAdapter(t, input)
			expected := rootPath
			if nestedExists {
				expected = nestedPath
			}
			found := false
			for _, source := range observed.Sources() {
				if source.Kind() == mcpeffective.SourceImport && source.Path() == expected && source.DefinesSelectedName() {
					found = true
				}
			}
			if !found || reads[expected] != 1 || nestedExists && reads[rootPath] != 0 {
				t.Fatal("OpenCode content/existence diverged or reread its chosen candidate", reads)
			}
		})
	}
}

func TestObservePiAdapterCancellationDiscardsPartialEvidence(t *testing.T) {
	for _, boundary := range []string{"entry", "normal", "codex", "opencode"} {
		t.Run(boundary, func(t *testing.T) {
			input := adapterReadFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input.Context = ctx
			if boundary == "entry" {
				cancel()
			}
			input.ReadFile = func(_ context.Context, path string, _ int64) ([]byte, bool, error) {
				if ctx.Err() != nil {
					t.Fatal("callback invoked after cancellation")
				}
				if boundary == "normal" {
					cancel()
				}
				if path == input.SelectedPath {
					return []byte(`{"imports":["codex","opencode"],"mcpServers":{"context7":{"command":"node"}}}`), true, nil
				}
				if path == importCandidates(importCodex, input.HomeDir, input.WorkDir)[0] && boundary == "codex" || path == filepath.Join(input.WorkDir, "opencode.json") && boundary == "opencode" {
					cancel()
				}
				return nil, false, nil
			}
			if _, err := ObservePiAdapter(input); !errors.Is(err, context.Canceled) {
				t.Fatal("canceled read produced usable partial evidence", err)
			}
		})
	}
}

func TestObservePiAdapterReadLimitIsOpaqueWithoutFallback(t *testing.T) {
	input := adapterReadFixture(t)
	input.ReadFile = func(_ context.Context, path string, _ int64) ([]byte, bool, error) {
		if path == input.SelectedPath {
			return nil, false, filesnapshot.ErrLimitExceeded
		}
		return nil, false, nil
	}
	if observed := mustObservePiAdapter(t, input); observed.State() != mcpeffective.StateUnobservable {
		t.Fatal("read failure became an absent source", observed.State())
	}
}
