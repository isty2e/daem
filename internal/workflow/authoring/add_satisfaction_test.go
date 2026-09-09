package authoring

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestSatisfiedMCPBindingStillAddsMissingProvider(t *testing.T) {
	original := []byte(`version = 1
targets = ["pi"]

[[mcp_server]]
name = "local"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	change, err := BuildAddMCPServerChange(ManifestDocument{Content: original}, AddMCPServerRequest{
		Name: "local", Command: "node", Args: []string{"server.js"}, Targets: []string{"pi"}, Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if change.ChangeKind != "append extension resource" || bytes.Equal(change.Content, original) {
		t.Fatalf("provider insertion = %#v", change)
	}
	servers, err := declarationcodec.ScanMCPServerBlocks(change.Content)
	if err != nil || len(servers) != 1 || servers[0].Server.Name != "local" {
		t.Fatalf("server duplication: %#v, %v", servers, err)
	}
	providers, err := declarationcodec.ScanExtensionBlocks(change.Content)
	if err != nil || len(providers) != 1 || providers[0].Extension.Carrier != "pi-package" {
		t.Fatalf("provider insertion: %#v, %v", providers, err)
	}
	if strings.Contains(change.ManifestBlock, "[[mcp_server]]") {
		t.Fatal("unchanged binding was presented as an inserted block")
	}
}

func TestSatisfiedExtensionUsesEffectiveSubjectDefaults(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]
[defaults]
scope = "project"

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`)
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		t.Fatal(err)
	}
	content, kind, err := ApplyAddExtensionToManifest(original, declaration.Extension{
		ID: "context7", Carrier: "claude-code-plugin", Targets: []string{"claude-code"}, Scope: "project", Source: declaration.ExtensionSource{Marketplace: "context7@market"},
	}, header)
	if err != nil || kind != "unchanged" || !bytes.Equal(content, original) {
		t.Fatalf("defaulted subject = %q, %q, %v", content, kind, err)
	}
}

func TestNamedGroupAddPreservesMemberSetAndConflictPolicy(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"oracle", "review"} {
		writeTestFile(t, filepath.Join(root, "skills", name), "SKILL.md", "---\nname: "+name+"\ndescription: Review work.\n---\n")
	}
	original := []byte("version = 1\ntargets = [\"codex\", \"claude-code\"]\n\n" + declarationcodec.RenderSkillGroupBlock(declarationcodec.SkillGroup{
		Names: []string{"oracle", "review"}, Source: declarationcodec.SkillSource{Path: "skills", Mode: "vendor"}, Targets: []string{"codex"},
	}))
	document := ManifestDocument{Root: root, Content: original}
	request := AddSkillGroupRequest{SourceArg: filepath.Join(root, "skills"), Names: []string{"review", "oracle"}, Targets: []string{"codex"}, Scope: "project"}
	change, err := BuildAddSkillGroupChange(document, request)
	if err != nil || change.ChangeKind != "unchanged" || !bytes.Equal(change.Content, original) {
		t.Fatalf("same group: %#v, %v", change, err)
	}
	request.Targets = []string{"claude-code"}
	change, err = BuildAddSkillGroupChange(document, request)
	if err != nil || change.ChangeKind != "update skill_group targets" {
		t.Fatalf("group target merge: %#v, %v", change, err)
	}
	groups, err := declarationcodec.ScanSkillGroupBlocks(change.Content)
	if err != nil || len(groups) != 1 || len(groups[0].Group.Targets) != 2 {
		t.Fatalf("merged groups=%#v error=%v", groups, err)
	}
	request.Targets = []string{"codex"}
	request.Names = []string{"oracle"}
	if _, err := BuildAddSkillGroupChange(document, request); err == nil {
		t.Fatal("overlapping different member set was silently regrouped")
	}
}
