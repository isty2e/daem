package authoring

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestSkillSourceNormalizesAcceptedSourceForms(t *testing.T) {
	tempDir := t.TempDir()
	localSkillPath := filepath.Join(tempDir, "skills", "local-review")
	writeTestFile(t, localSkillPath, "SKILL.md", "---\nname: local-review\ndescription: Local\n---\n")

	for _, testCase := range []struct {
		name        string
		request     AddSkillRequest
		wantSource  declarationcodec.SkillSource
		wantName    string
		wantErrText string
	}{
		{
			name: "full git url plus path",
			request: AddSkillRequest{
				SourceArg:  "https://github.com/owner/repo.git",
				SourcePath: "skills/oracle",
				Ref:        "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "https://github.com/owner/repo.git",
				Path: "skills/oracle",
				Ref:  "main",
			},
			wantName: "oracle",
		},
		{
			name: "full git url root defaults",
			request: AddSkillRequest{
				SourceArg: "https://github.com/owner/repo.git",
				Ref:       "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "https://github.com/owner/repo.git",
				Path: ".",
				Ref:  "main",
			},
			wantName: "repo",
		},
		{
			name: "ssh git url plus path",
			request: AddSkillRequest{
				SourceArg:  "git@github.com:owner/repo.git",
				SourcePath: "skills/oracle",
				Ref:        "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "git@github.com:owner/repo.git",
				Path: "skills/oracle",
				Ref:  "main",
			},
			wantName: "oracle",
		},
		{
			name: "scp-like non-default username",
			request: AddSkillRequest{
				SourceArg:  "alice@example.com:owner/repo.git",
				SourcePath: "skills/oracle",
				Ref:        "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "alice@example.com:owner/repo.git",
				Path: "skills/oracle",
				Ref:  "main",
			},
			wantName: "oracle",
		},
		{
			name: "native absolute git repository",
			request: AddSkillRequest{
				SourceArg: localSkillPath,
				Ref:       "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  localSkillPath,
				Path: ".",
				Ref:  "main",
			},
			wantName: "local-review",
		},
		{
			name: "github owner repo path shorthand",
			request: AddSkillRequest{
				SourceArg: "owner/repo/skills/oracle",
				Ref:       "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "https://github.com/owner/repo.git",
				Path: "skills/oracle",
				Ref:  "main",
			},
			wantName: "oracle",
		},
		{
			name: "github owner repo root shorthand",
			request: AddSkillRequest{
				SourceArg: "owner/repo",
				Ref:       "main",
			},
			wantSource: declarationcodec.SkillSource{
				Git:  "https://github.com/owner/repo.git",
				Path: ".",
				Ref:  "main",
			},
			wantName: "repo",
		},
		{
			name: "local path",
			request: AddSkillRequest{
				SourceArg: localSkillPath,
			},
			wantSource: declarationcodec.SkillSource{
				Path: filepath.ToSlash(filepath.Join("skills", "local-review")),
				Mode: "vendor",
			},
			wantName: "local-review",
		},
		{
			name: "github path shorthand with explicit path",
			request: AddSkillRequest{
				SourceArg:  "owner/repo/skills/oracle",
				SourcePath: "skills/other",
				Ref:        "main",
			},
			wantErrText: "do not combine owner/repo/path shorthand with --path",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			skill, err := SkillFromAddRequest(testCase.request, tempDir)
			if testCase.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErrText) {
					t.Fatalf("err = %v, want %q", err, testCase.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("SkillFromAddRequest returned error: %v", err)
			}
			if skill.Source != testCase.wantSource {
				t.Fatalf("source = %#v, want %#v", skill.Source, testCase.wantSource)
			}
			if skill.Name != testCase.wantName {
				t.Fatalf("name = %q, want %q", skill.Name, testCase.wantName)
			}
		})
	}
}

func TestSkillSourceRejectsCredentialBearingGitWithoutDisclosure(t *testing.T) {
	secret := "synthetic-secret"
	_, err := SkillFromAddRequest(AddSkillRequest{
		SourceArg: "https://user:" + secret + "@example.com/repo.git",
		Ref:       "main",
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must not contain userinfo") {
		t.Fatalf("error = %v, want userinfo rejection", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error disclosed credential: %v", err)
	}
}

func TestSkillSourceKeepsExistingOwnerRepoPathLocal(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	writeTestFile(t, filepath.Join(tempDir, "owner", "repo"), "SKILL.md", "---\nname: repo\ndescription: Local\n---\n")

	skill, err := SkillFromAddRequest(AddSkillRequest{
		SourceArg: "owner/repo",
	}, tempDir)
	if err != nil {
		t.Fatalf("SkillFromAddRequest returned error: %v", err)
	}

	wantSource := declarationcodec.SkillSource{
		Path: "owner/repo",
		Mode: "vendor",
	}
	if skill.Source != wantSource {
		t.Fatalf("source = %#v, want %#v", skill.Source, wantSource)
	}
	if skill.Name != "repo" {
		t.Fatalf("name = %q, want repo", skill.Name)
	}
}

func TestSkillGroupSourceInput(t *testing.T) {
	tempDir := t.TempDir()
	writeTestFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	group, err := SkillGroupFromAddRequest(AddSkillGroupRequest{
		SourceArg: filepath.Join(tempDir, "skills"),
		Names:     []string{"oracle", "review"},
		Targets:   []string{"codex"},
	}, tempDir)
	if err != nil {
		t.Fatalf("SkillGroupFromAddRequest returned error: %v", err)
	}
	if group.Source != (declarationcodec.SkillSource{Path: "skills", Mode: "vendor"}) {
		t.Fatalf("source = %#v, want local skills source", group.Source)
	}
	if strings.Join(group.Names, ",") != "oracle,review" {
		t.Fatalf("names = %#v, want oracle and review", group.Names)
	}

	linked, err := SkillGroupFromAddRequest(AddSkillGroupRequest{
		SourceArg: filepath.Join(tempDir, "skills"),
		Names:     []string{"oracle"},
		Mode:      "link",
	}, tempDir)
	if err != nil {
		t.Fatalf("SkillGroupFromAddRequest link returned error: %v", err)
	}
	if linked.Portable == nil || *linked.Portable {
		t.Fatalf("portable = %#v, want false pointer for link mode", linked.Portable)
	}

	gitGroup, err := SkillGroupFromAddRequest(AddSkillGroupRequest{
		SourceArg: "owner/repo/skills",
		Names:     []string{"oracle"},
		Ref:       "main",
	}, tempDir)
	if err != nil {
		t.Fatalf("SkillGroupFromAddRequest git returned error: %v", err)
	}
	wantGitSource := declarationcodec.SkillSource{
		Git:  "https://github.com/owner/repo.git",
		Path: "skills",
		Ref:  "main",
	}
	if gitGroup.Source != wantGitSource {
		t.Fatalf("source = %#v, want %#v", gitGroup.Source, wantGitSource)
	}

	_, err = SkillGroupFromAddRequest(AddSkillGroupRequest{
		SourceArg:  "owner/repo/skills",
		SourcePath: "other",
		Names:      []string{"oracle"},
		Ref:        "main",
	}, tempDir)
	if err == nil || !strings.Contains(err.Error(), "do not combine owner/repo/path shorthand with --path") {
		t.Fatalf("err = %v, want owner/repo/path conflict", err)
	}
}

func TestInstructionSourceInputScopeRules(t *testing.T) {
	tempDir := t.TempDir()
	projectSource := filepath.Join(tempDir, "instructions", "project.md")
	globalSource := filepath.Join(tempDir, "instructions", "global.md")
	sharedSource := filepath.Join(tempDir, "instructions", "shared.md")
	writeTestFile(t, filepath.Dir(projectSource), filepath.Base(projectSource), "Project guidance.\n")
	writeTestFile(t, filepath.Dir(globalSource), filepath.Base(globalSource), "Global guidance.\n")
	writeTestFile(t, filepath.Dir(sharedSource), filepath.Base(sharedSource), "Shared guidance.\n")

	projectInstruction, err := InstructionFromAddRequest(AddInstructionRequest{
		Name:      "project",
		SourceArg: projectSource,
	}, tempDir, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("InstructionFromAddRequest project returned error: %v", err)
	}
	if projectInstruction.Source.Path != "instructions/project.md" {
		t.Fatalf("project source path = %q, want relative path", projectInstruction.Source.Path)
	}

	globalInstruction, err := InstructionFromAddRequest(AddInstructionRequest{
		Name:      "global",
		SourceArg: globalSource,
	}, tempDir, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("InstructionFromAddRequest global returned error: %v", err)
	}
	if globalInstruction.Source.Path != filepath.ToSlash(globalSource) {
		t.Fatalf("global source path = %q, want absolute path", globalInstruction.Source.Path)
	}

	inheritedGlobal, err := InstructionFromAddRequest(AddInstructionRequest{
		Name:      "shared",
		SourceArg: sharedSource,
	}, tempDir, declaration.ManifestHeader{
		Defaults: declaration.Defaults{Scope: "global"},
	})
	if err != nil {
		t.Fatalf("InstructionFromAddRequest shared returned error: %v", err)
	}
	if inheritedGlobal.Source.Path != filepath.ToSlash(sharedSource) {
		t.Fatalf("shared source path = %q, want absolute path for global default", inheritedGlobal.Source.Path)
	}
}
