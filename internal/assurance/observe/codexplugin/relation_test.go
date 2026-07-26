package codexplugin

import (
	"os"
	"path/filepath"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

func TestCorrelateConfigUsesOnlyExactSelectedCodexRelation(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantState   observerelation.CorrelationState
		wantFailure bool
	}{
		{
			name:      "exact selected relation",
			content:   `[plugins."documents@official"]`,
			wantState: observerelation.StateExactCorrelation,
		},
		{
			name:      "disabled exact relation remains present",
			content:   "[plugins.\"documents@official\"]\nenabled = false\n",
			wantState: observerelation.StateExactCorrelation,
		},
		{
			name:      "exact pre-existing relation is exact without prior state",
			content:   `[plugins."documents@official"]`,
			wantState: observerelation.StateExactCorrelation,
		},
		{
			name:      "same plugin other marketplace is not exact",
			content:   `[plugins."documents@private"]`,
			wantState: observerelation.StateMissing,
		},
		{
			name:      "unrelated malformed row is ignored",
			content:   "plugins.noise = \"unsupported\"\n",
			wantState: observerelation.StateMissing,
		},
		{
			name:        "selected malformed row fails closed",
			content:     "plugins.\"documents@official\" = \"unsupported\"\n",
			wantFailure: true,
		},
		{
			name:        "unsupported plugins container fails closed",
			content:     "plugins = \"unsupported\"\n",
			wantFailure: true,
		},
		{
			name:      "missing plugins table is exact absence",
			content:   "model = \"gpt-test\"\n",
			wantState: observerelation.StateMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "config.toml")
			if err := os.WriteFile(configPath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			observation, err := ObserveConfigFile(configPath)
			if err != nil {
				t.Fatalf("ObserveConfigFile: %v", err)
			}
			result, err := CorrelateConfig(
				mustCodexCorrelationKey(t, "documents@official"),
				observation,
			)
			if test.wantFailure {
				if err == nil {
					t.Fatalf("CorrelateConfig returned state %q, want failure", result.State())
				}
				return
			}
			if err != nil {
				t.Fatalf("CorrelateConfig: %v", err)
			}
			if result.State() != test.wantState {
				t.Fatalf("state = %q, want %q", result.State(), test.wantState)
			}
		})
	}
}

func TestCorrelateConfigTreatsMissingConfigAsExactAbsence(t *testing.T) {
	observation, err := ObserveConfigFile(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := CorrelateConfig(
		mustCodexCorrelationKey(t, "documents@official"),
		observation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.State() != observerelation.StateMissing {
		t.Fatalf("state = %q, want missing", result.State())
	}
}

func mustCodexCorrelationKey(
	t *testing.T,
	selector string,
) observerelation.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"codex.plugin-carrier",
		"documents",
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(selector)
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:documents")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
