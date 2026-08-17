package codexplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestClassifySnapshotErrorDistinguishesIdentityAndBudget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want observecontribution.SourceContributionReason
	}{
		{filesnapshot.ErrChanged, observecontribution.SourceContributionReasonArtifactUnstable},
		{filesnapshot.ErrLimitExceeded, observecontribution.SourceContributionReasonArtifactBudgetExceeded},
		{filesnapshot.ErrSymlink, observecontribution.SourceContributionReasonArtifactPathBlocked},
		{filesnapshot.ErrNotRegular, observecontribution.SourceContributionReasonUnsupportedShape},
		{os.ErrNotExist, observecontribution.SourceContributionReasonArtifactUnavailable},
		{context.Canceled, observecontribution.SourceContributionReasonNone},
	}
	for _, testCase := range cases {
		if got := classifySnapshotError(testCase.err); got != testCase.want {
			t.Fatalf("classifySnapshotError(%v) = %q, want %q", testCase.err, got, testCase.want)
		}
	}
}

func TestObservationBudgetConsumeNamesOverflows(t *testing.T) {
	t.Parallel()
	budget := &observationBudget{}
	if budget.consumeNames([]string{"local"}) {
		t.Fatal("single name exhausted the observation budget")
	}
	overflow := make([]string, MaximumObservationEntries)
	for index := range overflow {
		overflow[index] = "x"
	}
	if !budget.consumeNames(overflow) || !budget.exceeded {
		t.Fatal("want entry overflow to exhaust the observation budget")
	}

	nameBudget := &observationBudget{}
	longName := string(make([]byte, MaximumObservationEntryNameBytes+1))
	if !nameBudget.consumeNames([]string{longName}) || !nameBudget.exceeded {
		t.Fatal("want overlong entry name to exhaust the observation budget")
	}
}

func TestObserveConfiguredPluginContributionsOmitsCanceledRows(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{"mcpServers": {"local": {}}}`)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	observations := ObserveConfiguredPluginContributions(
		ctx,
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
	)
	if len(observations) != 0 {
		t.Fatalf("observations = %#v, want none after cancellation", observations)
	}
}

func TestObserveConfiguredPluginContributionsBlocksOversizedManifest(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	path := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := file.Truncate(MaximumContributionFileBytes + 1); err != nil {
		t.Fatalf("Truncate returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want budget-exceeded blocker", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksSpecialFileManifest(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	path := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonUnsupportedShape ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want unsupported special-file shape", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksCacheCardinalityOverflow(t *testing.T) {
	homeDirectory := t.TempDir()
	cacheBase := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "alpha")
	if err := os.MkdirAll(cacheBase, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	for index := range MaximumObservationEntries + 1 {
		if err := os.Mkdir(filepath.Join(cacheBase, versionName(index)), 0o700); err != nil {
			t.Fatalf("Mkdir returned error: %v", err)
		}
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "beta", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"local": {}}
}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want two", observations)
	}
	for index, observation := range observations {
		row := firstDiagnosticRow(t, observation)
		if row.State() != observecontribution.SourceContributionBlocked ||
			row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
			row.HasContribution() {
			t.Fatalf("observation[%d] = %#v, want budget-exceeded blocker", index, observation)
		}
	}
}

func versionName(index int) string {
	return "v" + itoaAtLeast(index)
}

func itoaAtLeast(index int) string {
	const digits = "0123456789"
	if index < 10 {
		return digits[index : index+1]
	}
	return itoaAtLeast(index/10) + digits[index%10:index%10+1]
}

func TestObservationCanceledMatchesContextErrors(t *testing.T) {
	t.Parallel()
	if !observationCanceled(context.Canceled) || !observationCanceled(context.DeadlineExceeded) {
		t.Fatal("want canceled and deadline errors to omit rows")
	}
	if observationCanceled(errors.New("read failed")) || observationCanceled(nil) {
		t.Fatal("non-cancellation errors must not omit rows")
	}
}
