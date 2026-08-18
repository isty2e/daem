package codexplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	byteBudget := &observationBudget{}
	if byteBudget.consumeSnapshotBytes(MaximumObservationSnapshotBytes) || byteBudget.exceeded {
		t.Fatal("exact aggregate snapshot budget must be admitted")
	}
	if !byteBudget.consumeSnapshotBytes(1) || !byteBudget.exceeded {
		t.Fatal("want aggregate snapshot overflow to exhaust the observation budget")
	}

	keepBudget := &observationBudget{}
	for range MaximumObservationEntries {
		if keepBudget.consumeKeep() {
			t.Fatal("exact entry budget must admit consumeKeep")
		}
	}
	if !keepBudget.consumeKeep() || !keepBudget.exceeded {
		t.Fatal("want consumeKeep overflow to exhaust the observation budget")
	}
}

func TestObserveConfiguredPluginContributionsOmitsCanceledRows(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{"mcpServers": {"local": {}}}`)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	observations := observeIndependentPluginContributions(
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

	observations := observeIndependentPluginContributions(
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

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one aggregate budget blocker", observations)
	}
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want budget-exceeded blocker", observations[0])
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

func TestObserveConfiguredPluginContributionsCapsProviderOutput(t *testing.T) {
	homeDirectory := t.TempDir()
	keys := make([]string, MaximumObservationEntries+8)
	for index := range keys {
		keys[index] = versionName(index) + "@market"
	}

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, keys...),
	)
	if len(observations) != MaximumObservationEntries {
		t.Fatalf("observations = %d, want %d", len(observations), MaximumObservationEntries)
	}
	row := firstDiagnosticRow(t, observations[len(observations)-1])
	if row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("last observation = %#v, want budget-exceeded blocker", observations[len(observations)-1])
	}
}

func TestObserveConfiguredPluginContributionsReportsAllMaximumMissingProviders(t *testing.T) {
	homeDirectory := t.TempDir()
	keys := make([]string, MaximumObservationEntries)
	for index := range keys {
		keys[index] = versionName(index) + "@market"
	}

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, keys...),
	)
	if len(observations) != MaximumObservationEntries {
		t.Fatalf("observations = %d, want %d", len(observations), MaximumObservationEntries)
	}
	row := firstDiagnosticRow(t, observations[len(observations)-1])
	if row.State() != observecontribution.SourceContributionUnavailable ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactUnavailable {
		t.Fatalf("last observation = %#v, want unavailable rather than budget blocker", observations[len(observations)-1])
	}
}

func TestObserveConfiguredPluginContributionsBlocksOverdeepManifestPath(t *testing.T) {
	homeDirectory := t.TempDir()
	parts := make([]string, MaximumObservationPathComponents+1)
	for index := range parts {
		parts[index] = "a"
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "skills": ["./`+strings.Join(parts, `/`)+`"]
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want overdeep path budget blocker", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksManifestKeyOverflow(t *testing.T) {
	homeDirectory := t.TempDir()
	keys := make([]string, MaximumObservationEntries)
	for index := range keys {
		keys[index] = `"` + versionName(index) + `": {}`
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {`+strings.Join(keys, ",")+`}
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want budget-exceeded blocker", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsKeepsMaximumMCPKeysWithoutDoubleCharge(t *testing.T) {
	homeDirectory := t.TempDir()
	const mcpKeys = MaximumObservationEntries / 2
	keys := make([]string, mcpKeys)
	for index := range keys {
		keys[index] = `"` + versionName(index) + `": {}`
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {`+strings.Join(keys, ",")+`}
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	rows := observations[0].DiagnosticRows()
	if len(rows) != mcpKeys {
		t.Fatalf("rows = %d, want %d without double-charging MCP keys", len(rows), mcpKeys)
	}
	if rows[0].State() != observecontribution.SourceContributionDeclared ||
		rows[0].Kind() != observecontribution.SourceContributionMCPServer {
		t.Fatalf("observation = %#v, want declared MCP servers", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksSkillPathOverflow(t *testing.T) {
	homeDirectory := t.TempDir()
	paths := make([]string, MaximumObservationEntries+1)
	for index := range paths {
		paths[index] = `"./s` + versionName(index) + `"`
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "skills": [`+strings.Join(paths, ",")+`]
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want skill-path budget blocker", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsKeepsLargeInlineHookObjectDeclared(t *testing.T) {
	homeDirectory := t.TempDir()
	keys := make([]string, MaximumObservationEntries+1)
	for index := range keys {
		keys[index] = `"` + versionName(index) + `": {}`
	}
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "hooks": {`+strings.Join(keys, ",")+`}
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionDeclared ||
		!row.HasContribution() ||
		row.Kind() != observecontribution.SourceContributionHook ||
		row.Key() != "inline" {
		t.Fatalf("observation = %#v, want declared inline hook without materializing keys", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsKeepsInlineHookWithLargeNumberDeclared(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "hooks": {"x": 1e400}
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionDeclared ||
		row.Kind() != observecontribution.SourceContributionHook ||
		row.Key() != "inline" {
		t.Fatalf("observation = %#v, want declared inline hook for 1e400", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksUnsafeInlineHookKey(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "hooks": {"bad\u0000key": {}}
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonUnsupportedShape ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want unsupported inline hook key", observations[0])
	}
}

func TestPluginObservationSnapshotChargesAttemptedBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.json"), "content")
	writeFile(t, filepath.Join(root, "other.json"), "payload")
	file, err := os.Open(root)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	budget := &observationBudget{snapshotBytes: MaximumObservationSnapshotBytes - 7}
	observation := newPluginObservation(file, budget)
	content, exists, reason, err := observation.snapshot(t.Context(), "plugin.json")
	if err != nil || !exists || string(content) != "content" ||
		reason != observecontribution.SourceContributionReasonNone ||
		budget.snapshotBytes != MaximumObservationSnapshotBytes ||
		budget.exceeded {
		t.Fatalf(
			"snapshot = (%q, %t, %q, %v) bytes=%d exceeded=%t, want charged success at bound",
			content,
			exists,
			reason,
			err,
			budget.snapshotBytes,
			budget.exceeded,
		)
	}

	_, exists, reason, err = observation.snapshot(t.Context(), "other.json")
	if err != nil || exists ||
		reason != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		!budget.exceeded {
		t.Fatalf("second snapshot = (%t, %q, %v) exceeded=%t, want aggregate stop", exists, reason, err, budget.exceeded)
	}

	missingRoot := t.TempDir()
	missingFile, err := os.Open(missingRoot)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = missingFile.Close() })
	missingBudget := &observationBudget{}
	missing := newPluginObservation(missingFile, missingBudget)
	_, exists, reason, err = missing.snapshot(t.Context(), "missing.json")
	if err != nil || exists || reason != observecontribution.SourceContributionReasonNone ||
		missingBudget.snapshotBytes != 0 {
		t.Fatalf("missing snapshot charged %d, reason=%q err=%v", missingBudget.snapshotBytes, reason, err)
	}
}

func TestObserveConfiguredPluginDiagnosticsOmitsContributionsWhenConfigFillsBudget(t *testing.T) {
	configPath := writeCodexConfig(t, exactMaximumCodexPluginConfig())
	homeDirectory := t.TempDir()
	observation, contributions, err := ObserveConfiguredPluginDiagnostics(t.Context(), homeDirectory, configPath)
	if err != nil {
		t.Fatalf("ObserveConfiguredPluginDiagnostics returned error: %v", err)
	}
	if !observation.EntrySetObserved() || len(observation.Entries()) != MaximumObservationEntries {
		t.Fatalf("observation = %#v, want %d observed config keys", observation, MaximumObservationEntries)
	}
	if len(contributions) != 0 {
		t.Fatalf("contributions = %#v, want none after config fill", contributions)
	}
}

func TestObserveConfiguredPluginContributionsEmitsBlockerWhenCacheNamesFillKeep(t *testing.T) {
	homeDirectory := t.TempDir()
	cacheBase := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "alpha")
	if err := os.MkdirAll(cacheBase, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	for _, version := range []string{"1.0.0", "2.0.0"} {
		if err := os.Mkdir(filepath.Join(cacheBase, version), 0o700); err != nil {
			t.Fatalf("Mkdir returned error: %v", err)
		}
	}

	budget := &observationBudget{entries: MaximumObservationEntries - 2}
	observations := observeConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
		budget,
		false,
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one budget blocker", observations)
	}
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want budget-exceeded blocker instead of a dropped ambiguous row", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsEmitsBlockerWhenNextProviderHasNoSlot(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"local": {}}
}`)

	budget := &observationBudget{entries: MaximumObservationEntries - 2}
	observations := observeConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
		budget,
		false,
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want declared alpha plus a budget blocker for beta", observations)
	}
	alpha := firstDiagnosticRow(t, observations[0])
	if alpha.State() != observecontribution.SourceContributionDeclared ||
		!alpha.HasContribution() ||
		alpha.Kind() != observecontribution.SourceContributionMCPServer {
		t.Fatalf("first observation = %#v, want declared MCP keep", observations[0])
	}
	beta := firstDiagnosticRow(t, observations[1])
	if beta.State() != observecontribution.SourceContributionBlocked ||
		beta.Reason() != observecontribution.SourceContributionReasonArtifactBudgetExceeded ||
		string(beta.ProvidedBy()) != "beta@market" {
		t.Fatalf("second observation = %#v, want beta budget-exceeded blocker", observations[1])
	}
}

func TestObserveConfiguredPluginContributionsUnavailableUnreadableSkills(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	skills := filepath.Join(pluginRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skills, "review"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	writeFile(t, filepath.Join(skills, "review", "SKILL.md"), "---\nname: review\n---\n")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "skills": ["./skills"]
}`)
	if err := os.Chmod(skills, 0o000); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o700) })

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() == observecontribution.SourceContributionDeclared {
		t.Fatalf("observation = %#v, want fail-closed unreadable skills", observations[0])
	}
	if row.HasContribution() {
		t.Fatalf("observation = %#v, want no declared contributions", observations[0])
	}
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
