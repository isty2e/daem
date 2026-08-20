package diagnose

import (
	"context"
	"slices"
	"strings"
	"testing"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestInspectManifestSkillsRejectsGroupOverflowBeforeListing(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	set := diagnosticSkillSet(t, root, "glob:*")
	sets := make([]desiredskill.SkillSet, 1_025)
	for index := range sets {
		sets[index] = set
	}
	lister := &trackingSkillRootLister{}

	_, checks := inspectManifestSkills(
		context.Background(),
		lister,
		nil,
		sets,
		source.NewRootListingBudget(),
		desiredskill.NewExpansionBudget(),
	)

	if lister.totalCalls() != 0 {
		t.Fatalf("root listing calls = %d, want pre-listing rejection", lister.totalCalls())
	}
	assertSkillGroupBudgetCheck(t, checks, "groups observed=1025 limit=1024")
}

func TestInspectManifestSkillsAcceptsExactGroupLimitWithOneCachedListing(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	set := diagnosticSkillSet(t, root, "glob:alpha")
	sets := make([]desiredskill.SkillSet, 1_024)
	for index := range sets {
		sets[index] = set
	}
	lister := newTrackingSkillRootLister(t, map[source.Source][]string{root: {"alpha"}})

	skills, checks := inspectManifestSkills(
		context.Background(),
		lister,
		nil,
		sets,
		source.NewRootListingBudget(),
		desiredskill.NewExpansionBudget(),
	)

	if len(checks) != 0 {
		t.Fatalf("checks = %#v, want exact group limit accepted", checks)
	}
	if got := len(skills); got != 1_024 {
		t.Fatalf("inspected skills = %d, want 1024", got)
	}
	if lister.totalCalls() != 1 {
		t.Fatalf("root listing calls = %d, want one SourceID-cached listing", lister.totalCalls())
	}
}

func TestInspectManifestSkillsCachesListingsBySourceID(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	lister := newTrackingSkillRootLister(t, map[source.Source][]string{
		root: {"alpha", "beta"},
	})

	skills, checks := inspectManifestSkills(
		context.Background(),
		lister,
		nil,
		[]desiredskill.SkillSet{
			diagnosticSkillSet(t, root, "glob:alpha"),
			diagnosticSkillSet(t, root, "glob:beta"),
		},
		source.NewRootListingBudget(),
		desiredskill.NewExpansionBudget(),
	)

	if len(checks) != 0 {
		t.Fatalf("checks = %#v, want successful inspection", checks)
	}
	if lister.totalCalls() != 1 {
		t.Fatalf("root listing calls = %d, want one SourceID-cached listing", lister.totalCalls())
	}
	if got := skillInspectionNames(skills); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("inspected skills = %v, want [alpha beta]", got)
	}
}

func TestInspectManifestSkillsSharesRootListingBudgetAcrossSources(t *testing.T) {
	alphaRoot := sourcetest.Local(t, "alpha-skills", source.LocalSourceModeVendor)
	betaRoot := sourcetest.Local(t, "beta-skills", source.LocalSourceModeVendor)
	lister := newTrackingSkillRootLister(t, map[source.Source][]string{
		alphaRoot: {"alpha"},
		betaRoot:  {"beta"},
	})

	_, checks := inspectManifestSkills(
		context.Background(),
		lister,
		nil,
		[]desiredskill.SkillSet{
			diagnosticSkillSet(t, alphaRoot, "glob:*"),
			diagnosticSkillSet(t, betaRoot, "glob:*"),
		},
		source.NewRootListingBudget(),
		desiredskill.NewExpansionBudget(),
	)

	if len(checks) != 0 {
		t.Fatalf("checks = %#v, want successful inspection", checks)
	}
	if len(lister.budgets) != 2 || lister.budgets[0] != lister.budgets[1] {
		t.Fatalf("root listing budgets = %#v, want one phase-wide budget", lister.budgets)
	}
}

func TestInspectManifestSkillsReportsAggregateMatcherOverflowWithoutPartialGroups(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	childName := strings.Repeat("a", 255)
	lister := newTrackingSkillRootLister(t, map[source.Source][]string{root: {childName}})
	set := diagnosticSkillSet(t, root, "glob:"+strings.Repeat("*", 64<<10))
	sets := make([]desiredskill.SkillSet, 9)
	for index := range sets {
		sets[index] = set
	}
	direct := testfixture.Skill(t, desiredskill.Spec{
		Name:        "direct",
		Source:      sourcetest.Local(t, "skills/direct", source.LocalSourceModeVendor),
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
		Portable:    true,
	})

	skills, checks := inspectManifestSkills(
		context.Background(),
		lister,
		[]desiredskill.Skill{direct},
		sets,
		source.NewRootListingBudget(),
		desiredskill.NewExpansionBudget(),
	)

	assertSkillGroupBudgetCheck(t, checks, "matcher_work_units")
	if got := skillInspectionNames(skills); !slices.Equal(got, []string{"direct"}) {
		t.Fatalf("inspected skills = %v, want direct skills only after group overflow", got)
	}
	if lister.totalCalls() != 1 {
		t.Fatalf("root listing calls = %d, want one cached listing", lister.totalCalls())
	}
}

type trackingSkillRootLister struct {
	childrenByID map[artifact.SourceID][]string
	callsByID    map[artifact.SourceID]int
	budgets      []*source.RootListingBudget
}

func newTrackingSkillRootLister(
	t *testing.T,
	childrenBySource map[source.Source][]string,
) *trackingSkillRootLister {
	t.Helper()
	childrenByID := make(map[artifact.SourceID][]string, len(childrenBySource))
	for sourceSpec, children := range childrenBySource {
		sourceID, err := source.SourceIDFor(sourceSpec)
		if err != nil {
			t.Fatalf("SourceIDFor returned error: %v", err)
		}
		childrenByID[sourceID] = append([]string(nil), children...)
	}
	return &trackingSkillRootLister{
		childrenByID: childrenByID,
		callsByID:    make(map[artifact.SourceID]int),
	}
}

func (lister *trackingSkillRootLister) ListSourceRoot(
	_ context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (source.RootListing, error) {
	if lister.callsByID == nil {
		lister.callsByID = make(map[artifact.SourceID]int)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return source.RootListing{}, err
	}
	lister.callsByID[sourceID]++
	lister.budgets = append(lister.budgets, options.RootListingBudget())
	children := lister.childrenByID[sourceID]
	return source.NewRootListing(sourceSpec, "", artifact.ArtifactKindDirectory, children)
}

func (lister *trackingSkillRootLister) totalCalls() int {
	total := 0
	for _, calls := range lister.callsByID {
		total += calls
	}
	return total
}

func diagnosticSkillSet(t *testing.T, root source.Source, selectorExpression string) desiredskill.SkillSet {
	t.Helper()
	selector, err := desiredskill.ParseSelector(selectorExpression)
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}
	return testfixture.SkillSet(t, desiredskill.SkillSetSpec{
		Source:      root,
		Include:     []desiredskill.Selector{selector},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
	})
}

func skillInspectionNames(skills []desiredskill.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.ID().Name())
	}
	return names
}

func assertSkillGroupBudgetCheck(t *testing.T, checks []findings.Check, detail string) {
	t.Helper()
	if len(checks) != 1 {
		t.Fatalf("checks = %#v, want one skill-group budget error", checks)
	}
	check := checks[0]
	if check.Status != findings.CheckError || check.Name != skillGroupExpansionCheckName {
		t.Fatalf("check = %#v, want named error", check)
	}
	if !findings.HasCheckErrors(checks) {
		t.Fatalf("checks = %#v, want doctor error status", checks)
	}
	if !strings.Contains(check.Detail, detail) {
		t.Fatalf("detail = %q, want %q", check.Detail, detail)
	}
	if !strings.Contains(check.NextStep, "rerun daem doctor") {
		t.Fatalf("next step = %q, want doctor remediation", check.NextStep)
	}
}
