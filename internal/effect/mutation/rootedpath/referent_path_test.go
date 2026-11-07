//go:build darwin || linux || windows

package rootedpath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var errReferentTestBudget = errors.New("referent test budget exhausted")

type referentTestBudget struct {
	limit  int
	visits int
}

func (budget *referentTestBudget) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.limit-budget.visits {
		return errReferentTestBudget
	}
	budget.visits += count
	return nil
}

func TestResolveReferentPathBoundedObservesFinalAliasesWithoutMutation(t *testing.T) {
	base := referentTestRoot(t)
	directory := filepath.Join(base, "physical")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "settings.json")
	content := []byte("original settings\n")
	if err := os.WriteFile(file, content, 0o600); err != nil {
		t.Fatal(err)
	}
	directoryAlias := filepath.Join(base, "directory-alias")
	fileAlias := filepath.Join(directory, "file-alias")
	chainAlias := filepath.Join(base, "chain-alias")
	referentTestSymlink(t, "physical", directoryAlias)
	referentTestSymlink(t, "settings.json", fileAlias)
	referentTestSymlink(t, fileAlias, chainAlias)

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "file", path: file, want: file},
		{name: "directory", path: directory, want: directory},
		{name: "missing leaf", path: filepath.Join(directory, "new"), want: filepath.Join(directory, "new")},
		{name: "missing suffix", path: filepath.Join(directoryAlias, "new", "settings"), want: filepath.Join(directory, "new", "settings")},
		{name: "directory alias", path: directoryAlias, want: directory},
		{name: "file alias", path: fileAlias, want: file},
		{name: "ancestor and final alias", path: filepath.Join(directoryAlias, "file-alias"), want: file},
		{name: "absolute and relative chain", path: chainAlias, want: file},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := ResolveReferentPathBounded(test.path, 256, &referentTestBudget{limit: 10_000})
			if err != nil || resolved != test.want {
				t.Fatalf("referent = %q, %v; want %q", resolved, err, test.want)
			}
		})
	}

	entry, err := ResolveDestinationPathBounded(chainAlias, 256, &referentTestBudget{limit: 10_000})
	if err != nil || entry != chainAlias {
		t.Fatalf("directory-entry observation = %q, %v; want unchanged final alias %q", entry, err, chainAlias)
	}
	for alias, target := range map[string]string{directoryAlias: "physical", fileAlias: "settings.json", chainAlias: fileAlias} {
		actual, err := os.Readlink(alias)
		if err != nil || actual != target {
			t.Fatalf("alias %q changed to %q, %v", alias, actual, err)
		}
	}
	actual, err := os.ReadFile(file)
	if err != nil || string(actual) != string(content) {
		t.Fatalf("referent content changed to %q, %v", actual, err)
	}
}

func TestResolveReferentPathBoundedRefusesInvalidIngressBeforeTraversal(t *testing.T) {
	base := referentTestRoot(t)
	for _, test := range []struct {
		name     string
		path     string
		depth    int
		noBudget bool
	}{
		{name: "empty", depth: 256},
		{name: "relative", path: "relative", depth: 256},
		{name: "unclean", path: base + string(filepath.Separator) + ".", depth: 256},
		{name: "NUL", path: filepath.Join(base, "bad\x00name"), depth: 256},
		{name: "zero depth", path: base},
		{name: "negative depth", path: base, depth: -1},
		{name: "missing budget", path: base, depth: 256, noBudget: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := &referentTestBudget{limit: 10_000}
			var supplied PhysicalTraversalBudget = budget
			if test.noBudget {
				supplied = nil
			}
			resolved, err := ResolveReferentPathBounded(test.path, test.depth, supplied)
			if err == nil || resolved != "" || budget.visits != 0 {
				t.Fatalf("invalid ingress returned %q, %v with %d visits", resolved, err, budget.visits)
			}
		})
	}
}

func TestRootSelectorRequiresValidIngressAndTheOriginalTraversalBudget(t *testing.T) {
	base := referentTestRoot(t)
	for _, selector := range []string{"", "relative", base + string(filepath.Separator) + "bad\x00name"} {
		budget := &referentTestBudget{limit: 10_000}
		got, err := ResolveRootSelectorBounded(selector, 256, budget)
		if err == nil || got != "" || budget.visits != 0 {
			t.Errorf("invalid selector %q returned %q, %v after %d visits", selector, got, err, budget.visits)
		}
	}
	if got, err := ResolveRootSelectorBounded(base, 256, nil); err == nil || got != "" {
		t.Fatalf("missing budget returned %q, %v", got, err)
	}
	if got, err := ResolveRootSelectorBounded(base, 0, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
		t.Fatalf("invalid depth returned %q, %v", got, err)
	}
	if got, err := ResolveRootSelectorBounded(base, 1, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
		t.Fatalf("physical depth limit returned %q, %v", got, err)
	}
	if got, err := ResolveRootSelectorBounded(base, 256, &referentTestBudget{}); !errors.Is(err, errReferentTestBudget) || got != "" {
		t.Fatalf("exhausted budget returned %q, %v", got, err)
	}
	volumeRoot := filepath.VolumeName(base) + string(filepath.Separator)
	if got, err := ResolveRootSelectorBounded(volumeRoot, 256, &referentTestBudget{}); !errors.Is(err, errReferentTestBudget) || got != "" {
		t.Fatalf("filesystem root bypassed the traversal budget: %q, %v", got, err)
	}
}

func TestResolveReferentPathBoundedChargesFinalAliasAndPhysicalDepth(t *testing.T) {
	base := referentTestRoot(t)
	deep := filepath.Join(base, "one", "two", "three", "four", "five")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "settings")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	referentTestSymlink(t, file, alias)
	budget := &referentTestBudget{limit: 10_000}
	if resolved, err := ResolveReferentPathBounded(alias, 256, budget); err != nil || resolved != file {
		t.Fatalf("referent = %q, %v; want %q", resolved, err, file)
	}
	selectedDepth, err := absolutePathDepth(alias)
	if err != nil {
		t.Fatal(err)
	}
	if budget.visits <= selectedDepth {
		t.Fatalf("final target traversal was not charged: %d visits for %d selected components", budget.visits, selectedDepth)
	}
	for _, capacity := range []int{0, budget.visits - 1} {
		limited := &referentTestBudget{limit: capacity}
		resolved, err := ResolveReferentPathBounded(alias, 256, limited)
		if !errors.Is(err, errReferentTestBudget) || resolved != "" || limited.visits > capacity {
			t.Fatalf("budget %d returned %q, %v after %d visits", capacity, resolved, err, limited.visits)
		}
	}
	exact := &referentTestBudget{limit: budget.visits}
	if resolved, err := ResolveReferentPathBounded(alias, 256, exact); err != nil || resolved != file || exact.visits != budget.visits {
		t.Fatalf("exact budget returned %q, %v after %d visits", resolved, err, exact.visits)
	}
	for _, path := range []string{alias, filepath.Join(base, "missing", "one", "two", "three")} {
		resolved, err := ResolveReferentPathBounded(path, selectedDepth+1, &referentTestBudget{limit: 10_000})
		if err == nil || resolved != "" {
			t.Fatalf("excessive physical depth returned %q, %v", resolved, err)
		}
	}
}

func TestResolveReferentPathBoundedRefusesDanglingCyclesAndFileAncestors(t *testing.T) {
	base := referentTestRoot(t)
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(base, "dangling")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	referentTestSymlink(t, "missing", dangling)
	referentTestSymlink(t, "second", first)
	referentTestSymlink(t, "first", second)
	for _, path := range []string{dangling, filepath.Join(dangling, "child"), first, filepath.Join(file, "child")} {
		resolved, err := ResolveReferentPathBounded(path, 256, &referentTestBudget{limit: 10_000})
		if err == nil || resolved != "" {
			t.Fatalf("unresolvable path %q returned %q, %v", path, resolved, err)
		}
	}
}

func TestFutureReferentKeepsMissingAliasTargetsWithoutChangingStrictCalls(t *testing.T) {
	base := referentTestRoot(t)
	if err := os.Mkdir(filepath.Join(base, "vault"), 0o700); err != nil {
		t.Fatal(err)
	}
	referentTestSymlink(t, filepath.FromSlash("future/leaf"), filepath.Join(base, "hop"))
	for _, test := range []struct {
		name   string
		target string
		suffix string
		want   string
	}{
		{"leaf", filepath.FromSlash("vault/token"), "", filepath.Join(base, "vault", "token")},
		{"ancestor", "future-root", filepath.Join("child", "config.json"), filepath.Join(base, "future-root", "child", "config.json")},
		{"absolute", filepath.Join(base, "future-absolute", "leaf"), "", filepath.Join(base, "future-absolute", "leaf")},
		{"nested", "hop", "", filepath.Join(base, "future", "leaf")},
	} {
		t.Run(test.name, func(t *testing.T) {
			alias := filepath.Join(base, test.name)
			referentTestSymlink(t, test.target, alias)
			selected := filepath.Join(alias, test.suffix)
			budget := &referentTestBudget{limit: 10_000}
			strict := []func(string, int, PhysicalTraversalBudget) (string, error){ResolveReferentPathBounded, ResolveRootSelectorBounded}
			var kinds []FailureKind
			for _, resolve := range strict {
				got, err := resolve(selected, 256, budget)
				var failure *Failure
				if got != "" || !errors.As(err, &failure) {
					t.Fatalf("strict call admitted dangling path: %q, %v", got, err)
				}
				kinds = append(kinds, failure.Kind())
			}
			got, err := ResolveFutureReferentPathBounded(selected, 256, budget)
			if err != nil || got != test.want {
				t.Fatalf("future referent = %q, %v; want %q", got, err, test.want)
			}
			if _, err := os.Lstat(test.want); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("future lookup created an endpoint: %v", err)
			}
			for index, resolve := range strict {
				got, err := resolve(selected, 256, budget)
				var failure *Failure
				if got != "" || !errors.As(err, &failure) || failure.Kind() != kinds[index] {
					t.Fatalf("future mode changed a later strict call: %q, %v", got, err)
				}
			}
			if root, err := CaptureCanonicalRootBounded(alias, 256, budget); err == nil {
				_ = root.Close()
				t.Fatal("future path became captured root authority")
			}
		})
	}
}

func TestFutureReferentRefusesUndefinedAliasTargets(t *testing.T) {
	base := referentTestRoot(t)
	if err := os.WriteFile(filepath.Join(base, "file"), []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, target string }{
		{"missing-parent", filepath.FromSlash("missing/../outside")},
		{"file-parent", filepath.FromSlash("file/child")},
		{"directory-target", "file" + string(filepath.Separator)},
		{"loop", "loop"},
	} {
		alias := filepath.Join(base, test.name)
		referentTestSymlink(t, test.target, alias)
		if got, err := ResolveFutureReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
			t.Errorf("undefined future target %s returned %q, %v", test.name, got, err)
		}
	}
}

func TestFutureReferentRequiresSelectorIngressAndTraversalLimits(t *testing.T) {
	base := referentTestRoot(t)
	selected := filepath.Join(base, "missing")
	for _, invalid := range []string{"", "relative", base + string(filepath.Separator) + "nul\x00name"} {
		budget := &referentTestBudget{limit: 10_000}
		if got, err := ResolveFutureReferentPathBounded(invalid, 256, budget); err == nil || got != "" || budget.visits != 0 {
			t.Errorf("invalid selector %q returned %q, %v after %d visits", invalid, got, err, budget.visits)
		}
	}
	if got, err := ResolveFutureReferentPathBounded(selected, 256, nil); err == nil || got != "" {
		t.Fatalf("missing budget returned %q, %v", got, err)
	}
	if got, err := ResolveFutureReferentPathBounded(selected, 0, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
		t.Fatalf("invalid depth returned %q, %v", got, err)
	}
	if got, err := ResolveFutureReferentPathBounded(selected, 1, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
		t.Fatalf("depth limit returned %q, %v", got, err)
	}
	if got, err := ResolveFutureReferentPathBounded(selected, 256, &referentTestBudget{}); !errors.Is(err, errReferentTestBudget) || got != "" {
		t.Fatalf("exhausted budget returned %q, %v", got, err)
	}
	root := filepath.VolumeName(base) + string(filepath.Separator)
	budget := &referentTestBudget{limit: 1}
	if got, err := ResolveFutureReferentPathBounded(root, 256, budget); err != nil || got != root || budget.visits != 1 {
		t.Fatalf("comparison root = %q, %v after %d visits", got, err, budget.visits)
	}
}

func referentTestRoot(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDestinationPathBounded(filepath.Join(base, "probe"), 256, &referentTestBudget{limit: 10_000}); err != nil {
		if runtime.GOOS == "windows" && hasFailureKind(err, FailureUnsupportedPlatform) {
			t.Skipf("Windows rootedpath capability unavailable: %v", err)
		}
		t.Fatal(err)
	}
	return base
}

func referentTestSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("Windows symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
