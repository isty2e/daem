package archguard

import (
	"strings"
	"testing"
)

func TestFeatureMatrixUsesOneProductStatusAlgebra(t *testing.T) {
	publicMatrix := readRepoText(t, "docs/host-integrations.md")

	if findings := analyzeFeatureMatrixStatuses(publicMatrix); len(findings) != 0 {
		t.Fatalf("feature matrix status guard found drift:\n- %s", strings.Join(findings, "\n- "))
	}
}

func TestFeatureMatrixStatusGuardAcceptsScopedStatusesAndSyntax(t *testing.T) {
	publicMatrix := featureMatrixPublicFixture(
		"`supported` via `[[skill_group]]`",
		"`unsupported` core; `deferred` extension-backed",
		"`diagnostic`",
	)

	if findings := analyzeFeatureMatrixStatuses(publicMatrix); len(findings) != 0 {
		t.Fatalf("valid feature matrix produced findings:\n- %s", strings.Join(findings, "\n- "))
	}
}

func TestFeatureMatrixStatusGuardRejectsVocabularyAndTableDrift(t *testing.T) {
	tests := []struct {
		name         string
		publicMatrix string
		want         string
	}{
		{
			name:         "reason in target cell",
			publicMatrix: featureMatrixPublicFixture("`diagnostic` / `bridge-required`", "`supported`", "`diagnostic`"),
			want:         "non-status term \"bridge-required\"",
		},
		{
			name:         "policy in route state",
			publicMatrix: featureMatrixPublicFixture("`supported`", "`supported`", "`deferred`; future `observe-only`"),
			want:         "non-status term \"observe-only\"",
		},
		{
			name: "missing public label",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"- `supported`: meaning\n",
				"",
				1,
			),
			want: "has no canonical product status",
		},
		{
			name: "extra public label",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"## Non-Status Vocabulary",
				"- `host-unavailable`: leaked reason\n\n## Non-Status Vocabulary",
				1,
			),
			want: "appears in both product-status and non-status vocabularies",
		},
		{
			name: "missing target matrix",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				targetMatrixSectionHeading,
				"## Renamed Matrix",
				1,
			),
			want: "missing governed section",
		},
		{
			name: "malformed route table",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"| Route family | Current product state | What that means |",
				"| Route | State | Meaning |",
				1,
			),
			want: "has no governed route-state table",
		},
		{
			name: "malformed target data row",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"| Example | `supported` | `supported` | `supported` | `supported` | `unsupported` |",
				"| Example | `supported` | `supported` | `supported` | `supported` |",
				1,
			),
			want: "malformed Markdown table row",
		},
		{
			name:         "overlong target status cell",
			publicMatrix: featureMatrixPublicFixture("`supported` "+strings.Repeat("project-and-global ", 6), "`supported`", "`diagnostic`"),
			want:         "exceeds 80-byte compact-cell limit",
		},
		{
			name: "route table under renamed heading",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"## Example Route Summary",
				"## Example Routes",
				1,
			),
			want: "governed route-state table under unexpected heading",
		},
		{
			name: "overlapping status and reason",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"- `not-modeled`: meaning\n",
				"- `supported`: leaked status\n",
				1,
			),
			want: "appears in both product-status and non-status vocabularies",
		},
		{
			name: "fenced fake target table",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`")+"\n\n```markdown\n"+featureMatrixTargetFixture()+"\n```\n",
				targetMatrixSectionHeading,
				"## Renamed Matrix",
				1,
			),
			want: "missing governed section",
		},
		{
			name: "commented fake target table",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`")+"\n\n<!--\n"+featureMatrixTargetFixture()+"\n-->\n",
				targetMatrixSectionHeading,
				"## Renamed Matrix",
				1,
			),
			want: "missing governed section",
		},
		{
			name: "indented code fake target table",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`")+"\n\n    "+strings.ReplaceAll(featureMatrixTargetFixture(), "\n", "\n    ")+"\n",
				targetMatrixSectionHeading,
				"## Renamed Matrix",
				1,
			),
			want: "missing governed section",
		},
		{
			name:         "duplicate target section",
			publicMatrix: featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`") + "\n" + featureMatrixTargetFixture(),
			want:         "appears 2 times",
		},
		{
			name: "malformed public vocabulary bullet",
			publicMatrix: strings.Replace(
				featureMatrixPublicFixture("`supported`", "`supported`", "`diagnostic`"),
				"- `not-modeled`: meaning\n",
				"- `not-modeled` meaning\n",
				1,
			),
			want: "malformed vocabulary bullet",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := analyzeFeatureMatrixStatuses(test.publicMatrix)
			if !sliceContainsSubstring(findings, test.want) {
				t.Fatalf("findings = %#v, want substring %q", findings, test.want)
			}
		})
	}
}

func sliceContainsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func featureMatrixTargetFixture() string {
	return `## Target Surface And Operation Matrix

| Surface | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Example | ` + "`supported`" + ` | ` + "`supported`" + ` | ` + "`supported`" + ` | ` + "`supported`" + ` | ` + "`unsupported`" + ` |`
}

func featureMatrixPublicFixture(codex string, pi string, routeState string) string {
	return `# Product Feature Matrix

## Product Status Labels

- ` + "`supported`" + `: meaning
- ` + "`authoring-only`" + `: meaning
- ` + "`explicit`" + `: meaning
- ` + "`diagnostic`" + `: meaning
- ` + "`deferred`" + `: meaning
- ` + "`unsupported`" + `: meaning
- ` + "`blocked`" + `: meaning

## Non-Status Vocabulary

- ` + "`not-modeled`" + `: meaning
- ` + "`host-unavailable`" + `: meaning
- ` + "`bridge-required`" + `: meaning
- ` + "`observe-only`" + `: meaning
- ` + "`rejected`" + `: meaning
- ` + "`out-of-coverage`" + `: meaning

## Roadmap Posture

Text.

## Target Surface And Operation Matrix

| Surface | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Example | ` + codex + ` | ` + "`supported`" + ` | ` + "`supported`" + ` | ` + pi + ` | ` + "`unsupported`" + ` |

## Example Route Summary

| Route family | Current product state | What that means |
| --- | --- | --- |
| Example | ` + routeState + ` | Reason detail may say ` + "`bridge-required`" + `. |
`
}
