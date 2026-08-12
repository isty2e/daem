//go:build darwin || linux

package rootedpath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type boundedCaptureTestBudget struct {
	limit  int
	visits int
}

func (budget *boundedCaptureTestBudget) AdmitPathComponents(count int) error {
	if count < 0 || budget.visits+count > budget.limit {
		return errors.New("injected physical traversal budget exhausted")
	}
	budget.visits += count
	return nil
}

func TestChargeDestinationPathRequiresCompleteCapacityBeforeFilesystemUse(t *testing.T) {
	rootPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	root := mustCaptureRoot(t, rootPath)
	defer root.Close()
	relative, err := NewRelativeDestination("nested/entry")
	if err != nil {
		t.Fatalf("construct relative destination: %v", err)
	}
	destination, err := mustCapturedAuthority(t, root).Bind(relative)
	if err != nil {
		t.Fatalf("bind destination: %v", err)
	}
	depth, err := absolutePathDepth(filepath.Join(rootPath, "nested", "entry"))
	if err != nil {
		t.Fatalf("measure destination depth: %v", err)
	}

	insufficient := &boundedCaptureTestBudget{limit: depth - 1}
	if err := ChargeDestinationPath(destination, 256, insufficient); err == nil {
		t.Fatal("ChargeDestinationPath accepted incomplete traversal capacity")
	}
	if insufficient.visits != 0 {
		t.Fatalf("failed destination charge consumed %d visits", insufficient.visits)
	}
	exact := &boundedCaptureTestBudget{limit: depth}
	if err := ChargeDestinationPath(destination, 256, exact); err != nil {
		t.Fatalf("ChargeDestinationPath exact capacity: %v", err)
	}
	if exact.visits != depth {
		t.Fatalf("destination path visits = %d, want %d", exact.visits, depth)
	}
}

func TestBoundedEntryAuthorityRetainsTraversalBudgetForAcquire(t *testing.T) {
	selectedPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize selected root: %v", err)
	}
	selected := mustCaptureRoot(t, selectedPath)
	defer selected.Close()
	budget := &boundedCaptureTestBudget{limit: 1 << 20}
	authority, err := BindSelectedEntryAuthorityBounded(
		selected,
		selectedPath,
		filepath.Join(selectedPath, "state.json"),
		256,
		budget,
	)
	if err != nil {
		t.Fatalf("bind bounded entry authority: %v", err)
	}
	defer authority.Close()

	budget.limit = budget.visits
	if capability, err := authority.Acquire(); err == nil {
		_ = capability.Close()
		t.Fatal("bounded entry authority acquired without remaining traversal capacity")
	}
	budget.limit = 1 << 20
	capability, err := authority.Acquire()
	if err != nil {
		t.Fatalf("acquire bounded entry authority: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close bounded entry capability: %v", err)
	}
}

func TestCaptureDestinationBoundedRejectsDeepPhysicalAliasTarget(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	baseDepth, err := absolutePathDepth(base)
	if err != nil {
		t.Fatalf("measure test root depth: %v", err)
	}
	components := make([]string, 5)
	for index := range components {
		components[index] = "d"
	}
	deepParent := filepath.Join(append([]string{base}, components...)...)
	if err := os.MkdirAll(deepParent, 0o700); err != nil {
		t.Fatalf("create deep physical parent: %v", err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(deepParent, alias); err != nil {
		t.Fatalf("create short alias: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 1_000}

	_, _, err = CaptureDestinationBounded(
		filepath.Join(alias, "entry"),
		baseDepth+3,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("CaptureDestinationBounded error = %v, want physical-depth rejection", err)
	}
}

func TestCaptureDestinationBoundedStopsAtAggregateVisitLimit(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 1}

	_, _, err = CaptureDestinationBounded(
		filepath.Join(base, "missing", "entry"),
		256,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "injected physical traversal budget exhausted") {
		t.Fatalf("CaptureDestinationBounded error = %v, want aggregate-budget rejection", err)
	}
	if budget.visits != budget.limit {
		t.Fatalf("charged visits = %d, want exact limit %d", budget.visits, budget.limit)
	}
}

func TestCapturedRootBoundedAuthorityOperationsChargeBeforeValidation(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	depth, err := absolutePathDepth(path)
	if err != nil {
		t.Fatalf("measure test root depth: %v", err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatalf("capture test root: %v", err)
	}
	defer root.Close()

	insufficientAuthority := &boundedCaptureTestBudget{limit: depth - 1}
	if _, err := root.AuthorityBounded(insufficientAuthority); err == nil {
		t.Fatal("AuthorityBounded accepted insufficient validation capacity")
	}
	if insufficientAuthority.visits != 0 {
		t.Fatalf("failed authority validation charged %d visits, want atomic rejection", insufficientAuthority.visits)
	}
	authorityBudget := &boundedCaptureTestBudget{limit: depth}
	authority, err := root.AuthorityBounded(authorityBudget)
	if err != nil {
		t.Fatalf("AuthorityBounded: %v", err)
	}
	if authorityBudget.visits != depth {
		t.Fatalf("authority validation charged %d visits, want %d", authorityBudget.visits, depth)
	}

	relative, err := NewRelativeDestination("entry")
	if err != nil {
		t.Fatalf("construct relative destination: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("bind destination: %v", err)
	}
	insufficientAcquire := &boundedCaptureTestBudget{limit: 2*depth - 1}
	if capability, err := root.AcquireBounded(destination, 256, insufficientAcquire); err == nil {
		_ = capability.Close()
		t.Fatal("AcquireBounded accepted insufficient two-pass capacity")
	}
	if insufficientAcquire.visits != 0 {
		t.Fatalf("failed capability acquisition charged %d visits, want atomic rejection", insufficientAcquire.visits)
	}
	acquireBudget := &boundedCaptureTestBudget{limit: 2 * depth}
	capability, err := root.AcquireBounded(destination, 256, acquireBudget)
	if err != nil {
		t.Fatalf("AcquireBounded: %v", err)
	}
	defer capability.Close()
	if acquireBudget.visits != 2*depth {
		t.Fatalf("capability acquisition charged %d visits, want %d", acquireBudget.visits, 2*depth)
	}
	if opened, err := capability.OpenRootDirectory(); err == nil {
		_ = opened.Close()
		t.Fatal("bounded capability opened a destination without operation capacity")
	}

	insufficientWorking := &boundedCaptureTestBudget{limit: 2*depth - 1}
	if working, err := root.AcquireWorkingDirectoryBounded(insufficientWorking); err == nil {
		_ = working.Close()
		t.Fatal("AcquireWorkingDirectoryBounded accepted insufficient two-pass capacity")
	}
	if insufficientWorking.visits != 0 {
		t.Fatalf("failed working-directory acquisition charged %d visits, want atomic rejection", insufficientWorking.visits)
	}
	workingBudget := &boundedCaptureTestBudget{limit: 3 * depth}
	working, err := root.AcquireWorkingDirectoryBounded(workingBudget)
	if err != nil {
		t.Fatalf("AcquireWorkingDirectoryBounded: %v", err)
	}
	defer working.Close()
	opened, err := working.OpenDirectoryBounded(workingBudget)
	if err != nil {
		t.Fatalf("OpenDirectoryBounded: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close bounded working directory: %v", err)
	}
	if workingBudget.visits != 3*depth {
		t.Fatalf("bounded working directory charged %d visits, want %d", workingBudget.visits, 3*depth)
	}
}

func TestReserveDestinationAccessMatchesBoundedAcquisitionAndUse(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := NewRelativeDestination("nested/entry")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	rootDepth, err := capturedRootValidationPathComponents(&root.platform)
	if err != nil {
		t.Fatal(err)
	}
	destinationDepth, err := absolutePathDepth(filepath.Join(path, "nested", "entry"))
	if err != nil {
		t.Fatal(err)
	}
	want := 2*rootDepth + destinationDepth

	reservation := &boundedCaptureTestBudget{limit: want}
	if err := root.ReserveDestinationAccess(destination, 256, reservation); err != nil {
		t.Fatalf("reserve destination access: %v", err)
	}
	if reservation.visits != want {
		t.Fatalf("reserved visits = %d, want %d", reservation.visits, want)
	}

	execution := &boundedCaptureTestBudget{limit: want}
	capability, err := root.AcquireBounded(destination, 256, execution)
	if err != nil {
		t.Fatalf("acquire reserved destination: %v", err)
	}
	defer capability.Close()
	opened, err := capability.OpenRootDirectory()
	if err != nil {
		t.Fatalf("open reserved destination root: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close reserved destination root: %v", err)
	}
	if execution.visits != reservation.visits {
		t.Fatalf("execution visits = %d, reserved %d", execution.visits, reservation.visits)
	}
}

func TestCapturedRootChildObservationBatchIsDescriptorBoundAndBudgeted(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "present"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(path, "dangling")); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	depth, err := absolutePathDepth(path)
	if err != nil {
		t.Fatal(err)
	}
	wantVisits := 2 * (depth + 2)
	budget := &boundedCaptureTestBudget{limit: wantVisits}

	present, err := root.ChildrenExistNoFollow(
		t.Context(),
		[2]string{"present", "missing"},
		budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !present[0] || present[1] {
		t.Fatalf("child presence = %#v", present)
	}
	dangling, err := root.ChildrenExistNoFollow(
		t.Context(),
		[2]string{"dangling", "missing"},
		budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !dangling[0] || dangling[1] {
		t.Fatalf("dangling child presence = %#v", dangling)
	}
	if budget.visits != wantVisits {
		t.Fatalf("child observation visits = %d, want %d", budget.visits, wantVisits)
	}
	if _, err := root.ChildrenExistNoFollow(t.Context(), [2]string{"another", "missing"}, budget); err == nil ||
		!strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("exhausted child probe error = %v", err)
	}
}

func TestCapturedRootChildObservationRejectsReplacedBinding(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.Rename(path, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	depth, err := absolutePathDepth(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := &boundedCaptureTestBudget{limit: depth + 2}
	if _, err := root.ChildrenExistNoFollow(t.Context(), [2]string{"replacement", "other"}, budget); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replaced-root child observation error = %v, want %s", err, FailureRootReplaced)
	}
	if budget.visits != depth+2 {
		t.Fatalf("replaced-root child observation visits = %d, want %d", budget.visits, depth+2)
	}
}

func TestCapturedRootChildObservationAdmitsBudgetBeforeFilesystemValidation(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.Rename(path, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := &boundedCaptureTestBudget{limit: 0}
	if _, err := root.ChildrenExistNoFollow(t.Context(), [2]string{"replacement", "other"}, budget); err == nil ||
		!strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("child observation error = %v, want pre-observation budget rejection", err)
	}
}

func TestCapturedRootChildObservationCancellationBeforeWorkDoesNotCharge(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	budget := &boundedCaptureTestBudget{limit: 1_000}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := root.ChildrenExistNoFollow(ctx, [2]string{"child", "other"}, budget); !errors.Is(err, context.Canceled) {
		t.Fatalf("child observation error = %v, want cancellation", err)
	}
	if budget.visits != 0 {
		t.Fatalf("canceled child observation visits = %d, want 0", budget.visits)
	}
}

func TestCapturedRootChildObservationRejectsInvalidBatchBeforeWork(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRootNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, test := range []struct {
		name  string
		names [2]string
	}{
		{name: "empty", names: [2]string{}},
		{name: "duplicate", names: [2]string{"child", "child"}},
		{name: "path", names: [2]string{"nested/child", "other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := &boundedCaptureTestBudget{limit: 1_000}
			if _, err := root.ChildrenExistNoFollow(t.Context(), test.names, budget); err == nil {
				t.Fatal("invalid child observation batch succeeded")
			}
			if budget.visits != 0 {
				t.Fatalf("invalid child observation visits = %d, want 0", budget.visits)
			}
		})
	}
}

func TestCaptureDestinationBoundedChargesEachNativeComponentOnce(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	depth, err := absolutePathDepth(base)
	if err != nil {
		t.Fatalf("measure test root depth: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 100_000}

	root, _, err := CaptureDestinationBounded(
		filepath.Join(base, "entry"),
		256,
		budget,
	)
	if err != nil {
		t.Fatalf("CaptureDestinationBounded: %v", err)
	}
	defer root.Close()
	want := depth
	if budget.visits != want {
		t.Fatalf("charged visits = %d, want %d", budget.visits, want)
	}
}

func TestCaptureDestinationBoundedRejectsNativeInvalidAliasParentTraversal(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	regular := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(regular, []byte("content"), 0o600); err != nil {
		t.Fatalf("create regular file: %v", err)
	}

	for _, test := range []struct {
		name   string
		target string
	}{
		{name: "non-directory", target: "not-a-directory/.."},
		{name: "missing", target: "missing/.."},
	} {
		t.Run(test.name, func(t *testing.T) {
			alias := filepath.Join(base, "alias-"+test.name)
			if err := os.Symlink(test.target, alias); err != nil {
				t.Fatalf("create alias: %v", err)
			}
			selected := filepath.Join(alias, "entry")
			if _, err := os.Stat(filepath.Dir(selected)); err == nil {
				t.Fatalf("native path unexpectedly resolved")
			}
			budget := &boundedCaptureTestBudget{limit: 10_000}
			_, _, err := CaptureDestinationBounded(selected, 256, budget)
			if err == nil {
				t.Fatalf("CaptureDestinationBounded succeeded for native-invalid target %q", test.target)
			}
		})
	}
}

func TestCaptureDestinationBoundedRejectsNoncanonicalSelectedPath(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 10_000}
	_, _, err = CaptureDestinationBounded(
		base+string(filepath.Separator)+"missing"+string(filepath.Separator)+".."+
			string(filepath.Separator)+"entry",
		256,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "canonical lexical spelling") {
		t.Fatalf("CaptureDestinationBounded error = %v, want noncanonical-path rejection", err)
	}
}

func TestCaptureDestinationBoundedRejectsDanglingAliasTarget(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "missing-target"), alias); err != nil {
		t.Fatalf("create dangling alias: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 1_000}

	_, _, err = CaptureDestinationBounded(
		filepath.Join(alias, "entry"),
		256,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "alias target is unavailable") {
		t.Fatalf("CaptureDestinationBounded error = %v, want dangling-alias rejection", err)
	}
}

func TestCaptureDestinationBoundedPreservesAliasResolutionSemantics(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	target := filepath.Join(base, "target")
	nested := filepath.Join(base, "nested")
	for _, directory := range []string{target, nested} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory %q: %v", directory, err)
		}
	}
	if err := os.Symlink("target", filepath.Join(base, "relative")); err != nil {
		t.Fatalf("create relative alias: %v", err)
	}
	if err := os.Symlink("relative", filepath.Join(base, "chained")); err != nil {
		t.Fatalf("create chained alias: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "target"), filepath.Join(nested, "parent")); err != nil {
		t.Fatalf("create parent-traversing alias: %v", err)
	}

	for _, test := range []struct {
		name     string
		selected string
	}{
		{name: "relative", selected: filepath.Join(base, "relative", "missing", "entry")},
		{name: "chained", selected: filepath.Join(base, "chained", "missing", "entry")},
		{name: "parent traversal", selected: filepath.Join(nested, "parent", "missing", "entry")},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := &boundedCaptureTestBudget{limit: 10_000}
			root, destination, err := CaptureDestinationBounded(test.selected, 256, budget)
			if err != nil {
				t.Fatalf("CaptureDestinationBounded: %v", err)
			}
			defer root.Close()
			got, err := destination.LexicalPath()
			if err != nil {
				t.Fatalf("read bounded destination: %v", err)
			}
			want := filepath.Join(target, "missing", "entry")
			if got != want {
				t.Fatalf("bounded destination = %q, want %q", got, want)
			}
		})
	}
}

func TestCaptureDestinationBoundedAppliesDepthToResolvedAliasTarget(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	baseDepth, err := absolutePathDepth(base)
	if err != nil {
		t.Fatalf("measure test root depth: %v", err)
	}
	deep := filepath.Join(base, "one", "two")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("create depth-bound directory: %v", err)
	}
	if err := os.Symlink("..", filepath.Join(deep, "up")); err != nil {
		t.Fatalf("create parent alias: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 10_000}
	root, destination, err := CaptureDestinationBounded(
		filepath.Join(deep, "up", "entry"),
		baseDepth+2,
		budget,
	)
	if err != nil {
		t.Fatalf("CaptureDestinationBounded: %v", err)
	}
	defer root.Close()
	got, err := destination.LexicalPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "one", "entry")
	if got != want {
		t.Fatalf("resolved destination = %q, want %q", got, want)
	}
}

func TestCaptureDestinationBoundedRejectsAliasCycle(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	if err := os.Symlink("second", filepath.Join(base, "first")); err != nil {
		t.Fatalf("create first cycle alias: %v", err)
	}
	if err := os.Symlink("first", filepath.Join(base, "second")); err != nil {
		t.Fatalf("create second cycle alias: %v", err)
	}
	budget := &boundedCaptureTestBudget{limit: 100_000}

	_, _, err = CaptureDestinationBounded(
		filepath.Join(base, "first", "entry"),
		256,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "too many symbolic links") {
		t.Fatalf("CaptureDestinationBounded error = %v, want symlink-cycle rejection", err)
	}
}

func TestCaptureDestinationBindsMissingDescendantsToNearestExistingAncestor(t *testing.T) {
	ancestor := filepath.Join(t.TempDir(), "admitted")
	if err := os.Mkdir(ancestor, 0o700); err != nil {
		t.Fatalf("create admitted ancestor: %v", err)
	}
	selected := filepath.Join(ancestor, ".agents", "skills", "review")
	physicalAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		t.Fatalf("resolve admitted ancestor: %v", err)
	}
	want := filepath.Join(physicalAncestor, ".agents", "skills", "review")

	root, destination, err := CaptureDestination(selected)
	if err != nil {
		t.Fatalf("CaptureDestination returned error: %v", err)
	}
	defer root.Close()
	if got := destination.Root().PhysicalRoot(); got != physicalAncestor {
		t.Fatalf("captured physical root = %q, want %q", got, physicalAncestor)
	}
	if got := destination.Relative().Path(); got != ".agents/skills/review" {
		t.Fatalf("captured relative destination = %q, want %q", got, ".agents/skills/review")
	}
	if got, err := destination.LexicalPath(); err != nil || got != want {
		t.Fatalf("captured lexical destination = %q, error = %v, want %q", got, err, want)
	}
}

func TestCaptureDestinationRetainsPhysicalAncestorAfterAliasRetarget(t *testing.T) {
	parent := t.TempDir()
	admitted := filepath.Join(parent, "admitted")
	retargeted := filepath.Join(parent, "retargeted")
	for _, directory := range []string{admitted, retargeted} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create destination ancestor %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(admitted, alias); err != nil {
		t.Fatalf("create selected ancestor alias: %v", err)
	}

	root, destination, err := CaptureDestination(filepath.Join(alias, "missing", "entry"))
	if err != nil {
		t.Fatalf("CaptureDestination returned error: %v", err)
	}
	defer root.Close()
	capability, err := root.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected ancestor alias: %v", err)
	}
	if err := os.Symlink(retargeted, alias); err != nil {
		t.Fatalf("retarget selected ancestor alias: %v", err)
	}
	opened, err := capability.OpenRootDirectory()
	if err != nil {
		t.Fatalf("open retained root directory: %v", err)
	}
	defer opened.Close()
	openedInfo, err := opened.Stat()
	if err != nil {
		t.Fatalf("stat retained root directory: %v", err)
	}
	admittedInfo, err := os.Stat(admitted)
	if err != nil {
		t.Fatalf("stat admitted directory: %v", err)
	}
	if !os.SameFile(openedInfo, admittedInfo) {
		t.Fatalf("retained root no longer identifies the admitted ancestor")
	}
	retargetedInfo, err := os.Stat(retargeted)
	if err != nil {
		t.Fatalf("stat retargeted directory: %v", err)
	}
	if os.SameFile(openedInfo, retargetedInfo) {
		t.Fatalf("retained root followed the retargeted alias")
	}
}

func TestCaptureRootResolvesAliasOnceAndIssuesIndependentCapability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	alias := filepath.Join(filepath.Dir(root), "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create captured root alias: %v", err)
	}

	direct := mustCaptureRoot(t, root)
	defer direct.Close()
	throughAlias := mustCaptureRoot(t, alias)
	defer throughAlias.Close()
	directAuthority := mustCapturedAuthority(t, direct)
	aliasAuthority := mustCapturedAuthority(t, throughAlias)
	if !directAuthority.Equal(aliasAuthority) {
		t.Fatalf("alias authority %#v does not equal direct authority %#v", aliasAuthority, directAuthority)
	}

	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := aliasAuthority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := throughAlias.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if err := throughAlias.Close(); err != nil {
		t.Fatalf("close captured alias root: %v", err)
	}
	if err := capability.Validate(); err != nil {
		t.Fatalf("capability did not retain independent root witness: %v", err)
	}
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		t.Fatalf("OpenRootDirectory returned error: %v", err)
	}
	if err := capability.ValidateDirectoryHandle(rootFile.Fd()); err != nil {
		t.Fatalf("ValidateDirectoryHandle(root) returned error: %v", err)
	}
	if err := rootFile.Close(); err != nil {
		t.Fatalf("close duplicate root descriptor: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close capability: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("second capability close returned error: %v", err)
	}
	if !hasFailureKind(capability.Validate(), FailureRootUnavailable) {
		t.Fatalf("closed capability Validate error = %v, want %s", capability.Validate(), FailureRootUnavailable)
	}
}

func TestAuthorityProvenanceMatchesIndependentRecaptureAndRejectsReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}

	first := mustCaptureRoot(t, root)
	provenance, err := mustCapturedAuthority(t, first).Provenance()
	if err != nil {
		t.Fatalf("Provenance returned error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first capture: %v", err)
	}

	second := mustCaptureRoot(t, root)
	if err := provenance.Match(mustCapturedAuthority(t, second)); err != nil {
		t.Fatalf("independent recapture did not match persisted provenance: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second capture: %v", err)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move original root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	replacement := mustCaptureRoot(t, root)
	defer replacement.Close()
	if err := provenance.Match(mustCapturedAuthority(t, replacement)); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replacement match error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCapturedRootValidatesSelectedAliasStillNamesCapturedAuthority(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	other := filepath.Join(parent, "other")
	for _, directory := range []string{root, other} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create selected-root alias: %v", err)
	}
	captured := mustCaptureRoot(t, alias)
	defer captured.Close()
	if err := captured.ValidateSelection(alias); err != nil {
		t.Fatalf("ValidateSelection returned error for unchanged alias: %v", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected-root alias: %v", err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatalf("retarget selected-root alias: %v", err)
	}
	if err := captured.ValidateSelection(alias); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("retargeted selection error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCaptureRootNoFollowRejectsSelectedAndAncestorSymlinks(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatalf("create physical root: %v", err)
	}

	selectedLink := filepath.Join(parent, "selected")
	if err := os.Symlink(physical, selectedLink); err != nil {
		t.Fatalf("create selected-root symlink: %v", err)
	}
	if _, err := CaptureRootNoFollow(selectedLink); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("selected-root symlink error = %v, want %s", err, FailureRootReplaced)
	}

	ancestorLink := filepath.Join(parent, "ancestor")
	if err := os.Symlink(parent, ancestorLink); err != nil {
		t.Fatalf("create ancestor symlink: %v", err)
	}
	if _, err := CaptureRootNoFollow(filepath.Join(ancestorLink, "physical")); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("ancestor symlink error = %v, want %s", err, FailureRootReplaced)
	}

	captured, err := CaptureRootNoFollow(physical)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow physical root returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close no-follow root: %v", err)
	}
}

func TestNoFollowWorkingDirectoryCapabilityRejectsSymlinkRetargetToSameObject(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	selected := filepath.Join(parent, "selected")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatalf("create selected root: %v", err)
	}
	captured, err := CaptureRootNoFollow(selected)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow returned error: %v", err)
	}
	defer captured.Close()

	capability, err := captured.AcquireSelectedWorkingDirectory(selected)
	if err != nil {
		t.Fatalf("AcquireSelectedWorkingDirectory returned error: %v", err)
	}
	defer capability.Close()

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(selected, moved); err != nil {
		t.Fatalf("move selected root: %v", err)
	}
	if err := os.Symlink(moved, selected); err != nil {
		t.Fatalf("replace selected root with symlink: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("symlink retarget error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestSelectedWorkingDirectoryCapabilityRejectsRetargetedAlias(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	other := filepath.Join(parent, "other")
	for _, directory := range []string{root, other} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create selected-root alias: %v", err)
	}
	captured := mustCaptureRoot(t, alias)
	defer captured.Close()
	capability, err := captured.AcquireSelectedWorkingDirectory(alias)
	if err != nil {
		t.Fatalf("AcquireSelectedWorkingDirectory returned error: %v", err)
	}
	defer capability.Close()

	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected-root alias: %v", err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatalf("retarget selected-root alias: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("retargeted capability error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestWorkingDirectoryCapabilityRetainsIndependentRootWitness(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	capability, err := captured.AcquireWorkingDirectory()
	if err != nil {
		t.Fatalf("AcquireWorkingDirectory returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close captured root: %v", err)
	}
	directory, err := capability.OpenDirectory()
	if err != nil {
		t.Fatalf("OpenDirectory returned error: %v", err)
	}
	info, err := directory.Stat()
	if err != nil {
		t.Fatalf("stat opened working directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("opened working directory mode = %v, want directory", info.Mode())
	}
	if err := directory.Close(); err != nil {
		t.Fatalf("close opened working directory: %v", err)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replaced working-directory capability error = %v, want %s", err, FailureRootReplaced)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close working-directory capability: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("second working-directory capability close: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootUnavailable) {
		t.Fatalf("closed working-directory capability error = %v, want %s", err, FailureRootUnavailable)
	}
}

func TestCapturedRootRejectsReplacementAndForeignAuthority(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	defer captured.Close()
	authority := mustCapturedAuthority(t, captured)
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := captured.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	otherRoot := filepath.Join(parent, "other")
	if err := os.Mkdir(otherRoot, 0o700); err != nil {
		t.Fatalf("create other root: %v", err)
	}
	other := mustCaptureRoot(t, otherRoot)
	defer other.Close()
	otherDestination, err := mustCapturedAuthority(t, other).Bind(relative)
	if err != nil {
		t.Fatalf("bind other destination: %v", err)
	}
	if _, err := captured.Acquire(otherDestination); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("foreign destination Acquire error = %v, want %s", err, FailureInvalidDestination)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replaced root capability error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCommitCapabilityRejectsDescendantMountCrossing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	defer captured.Close()
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := mustCapturedAuthority(t, captured).Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := captured.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	foreign, err := os.Open("/dev")
	if err != nil {
		t.Skipf("open foreign mount: %v", err)
	}
	defer foreign.Close()
	if err := capability.ValidateDirectoryHandle(foreign.Fd()); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("foreign mount validation error = %v, want %s", err, FailureMountChanged)
	}
}

func TestDirectoryMountBoundaryRejectsForeignMount(t *testing.T) {
	root := t.TempDir()
	rootDirectory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDirectory.Close()
	boundary, err := CaptureDirectoryMountBoundary(rootDirectory.Fd())
	if err != nil {
		t.Fatalf("CaptureDirectoryMountBoundary: %v", err)
	}

	foreign, err := os.Open("/dev")
	if err != nil {
		t.Skipf("open foreign mount: %v", err)
	}
	defer foreign.Close()
	if err := boundary.ValidateDirectoryHandle(foreign.Fd()); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("foreign mount validation error = %v, want %s", err, FailureMountChanged)
	}

	filesystemRoot, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer filesystemRoot.Close()
	rootBoundary, err := CaptureDirectoryMountBoundary(filesystemRoot.Fd())
	if err != nil {
		t.Fatalf("capture filesystem-root boundary: %v", err)
	}
	if err := rootBoundary.ValidateEntryAt(filesystemRoot.Fd(), "dev"); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("foreign entry mount validation error = %v, want %s", err, FailureMountChanged)
	}
}

func TestDirectoryMountBoundaryObservesRestrictiveEntriesWithoutOpeningThem(t *testing.T) {
	root := t.TempDir()
	rootDirectory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDirectory.Close()
	boundary, err := CaptureDirectoryMountBoundary(rootDirectory.Fd())
	if err != nil {
		t.Fatalf("CaptureDirectoryMountBoundary: %v", err)
	}

	for _, test := range []struct {
		name   string
		create func(string) error
	}{
		{
			name: "regular",
			create: func(path string) error {
				return os.WriteFile(path, []byte("payload"), 0o600)
			},
		},
		{
			name: "directory",
			create: func(path string) error {
				return os.Mkdir(path, 0o700)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name)
			if err := test.create(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o700) })

			if err := boundary.ValidateEntryAt(rootDirectory.Fd(), test.name); err != nil {
				t.Fatalf("ValidateEntryAt: %v", err)
			}
		})
	}
}

func TestDirectoryMountBoundaryRejectsNonEntryNames(t *testing.T) {
	root := t.TempDir()
	rootDirectory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDirectory.Close()
	boundary, err := CaptureDirectoryMountBoundary(rootDirectory.Fd())
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", ".", "..", "nested/entry", "entry\x00suffix"} {
		if err := boundary.ValidateEntryAt(rootDirectory.Fd(), name); !hasFailureKind(err, FailureInvalidDestination) {
			t.Fatalf("ValidateEntryAt(%q) error = %v, want %s", name, err, FailureInvalidDestination)
		}
	}
}

func TestClosedCapturedRootCannotIssueCapability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	authority := mustCapturedAuthority(t, captured)
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	if _, err := captured.Acquire(destination); !hasFailureKind(err, FailureRootUnavailable) {
		t.Fatalf("closed root Acquire error = %v, want %s", err, FailureRootUnavailable)
	}
}

func mustCaptureRoot(t *testing.T, path string) *CapturedRoot {
	t.Helper()
	root, err := CaptureRoot(path)
	if err != nil {
		t.Fatalf("CaptureRoot(%q) returned error: %v", path, err)
	}
	return root
}

func mustCapturedAuthority(t *testing.T, root *CapturedRoot) Authority {
	t.Helper()
	authority, err := root.Authority()
	if err != nil {
		t.Fatalf("CapturedRoot.Authority returned error: %v", err)
	}
	return authority
}
