package declaration

import "testing"

func TestRepeatedSourceTablesKeepEachSkillSource(t *testing.T) {
	content := []byte(`version=1
targets=["codex"]
[[skill]]
name="one"
[skill.source]
path="/one"
mode="vendor"
[skill.target.codex]
install_to="~/.codex/skills"
[[skill]]
name="two"
[skill.source]
path="/two"
mode="vendor"
`)
	manifest, err := DecodeManifest(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Skills) != 2 || manifest.Skills[0].Source.Path != "/one" || manifest.Skills[1].Source.Path != "/two" {
		t.Fatal("source table bodies crossed array members", manifest.Skills)
	}
}
