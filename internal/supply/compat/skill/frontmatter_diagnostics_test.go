package skillcompat

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestFrontmatterDiagnosticsMatchesArtifactDiagnostics(t *testing.T) {
	for _, content := range []string{
		"---\nname: review\ndescription: Demo\n---\n",
		"---\nname: Not_Portable\nz-extra: true\na-extra: false\n---\n",
		"---\nname: different\ndescription: Demo\nallowed-tools: Read\n---\n",
		"---\n---\n",
	} {
		artifact := writeSkill(t, "review", content)
		frontmatter, err := ParseSkillFrontmatter([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		for _, selectedTarget := range append(target.SupportedTargets(), target.Target("unknown")) {
			got := FrontmatterDiagnostics(artifact.sourceID, "review", selectedTarget, frontmatter)
			want := artifact.diagnostics("review", selectedTarget)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("target %s: parsed diagnostics = %#v, want artifact diagnostics %#v", selectedTarget, got, want)
			}
		}
	}
}
