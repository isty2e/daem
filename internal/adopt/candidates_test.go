package adopt

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestCandidateSetOwnsNestedFactsAndDefensivelyDisclosesThem(t *testing.T) {
	sourceContent := []byte("instructions\n")
	skillTargets := []targetpkg.Target{targetpkg.TargetCodex}
	serverArgs := []string{"-y", "server"}
	serverEnv := map[string]string{"TOKEN": "TOKEN"}
	sources := []Source{{
		ResourceName: "instructions",
		Target:       targetpkg.TargetCodex,
		Scope:        targetpkg.ScopeProject,
		LivePath:     "AGENTS.md",
		SourcePath:   "/workspace/daem.d/instructions.md",
		Content:      sourceContent,
	}}
	skills := []Skill{{
		ResourceName: "review",
		InstallName:  "review",
		Target:       targetpkg.TargetCodex,
		Targets:      skillTargets,
		Scope:        targetpkg.ScopeProject,
		LivePath:     "/host/skills/review",
		ReadPath:     "/host/skills/review",
		SourcePath:   "/workspace/daem.d/skills/review",
		ContentHash:  artifact.HashFileContent([]byte("review")),
	}}
	servers := []MCPServer{{
		ResourceName: "context7",
		Target:       targetpkg.TargetCodex,
		Scope:        targetpkg.ScopeProject,
		LivePath:     ".codex/config.toml#/mcp_servers/context7",
		Command:      "npx",
		Args:         serverArgs,
		Env:          serverEnv,
	}}
	candidates, err := NewCandidateSet(
		sources,
		skills,
		[]Hook{{
			ResourceName: "lint",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     ".codex/hooks.json",
			Event:        "PreToolUse",
			Command:      "lint",
		}},
		servers,
		[]Scan{{
			ResourceName: "skill-root",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     "/host/skills",
			Status:       "scanned",
			Entries:      1,
			Imported:     1,
		}},
		[]Skipped{{LivePath: "/host/empty", Reason: "empty"}},
	)
	if err != nil {
		t.Fatal(err)
	}

	sourceContent[0] = 'X'
	skillTargets[0] = targetpkg.TargetClaudeCode
	serverArgs[0] = "--changed"
	serverEnv["TOKEN"] = "CHANGED"
	sources[0].ResourceName = "changed"
	skills[0].InstallName = "changed"
	servers[0].Command = "changed"
	assertCandidateSetUnchanged(t, candidates)

	disclosedSources := candidates.Sources()
	disclosedSkills := candidates.Skills()
	disclosedHooks := candidates.Hooks()
	disclosedServers := candidates.MCPServers()
	disclosedScans := candidates.Scans()
	disclosedSkipped := candidates.Skipped()
	disclosedSources[0].Content[0] = 'Y'
	disclosedSources[0].ResourceName = "changed"
	disclosedSkills[0].Targets[0] = targetpkg.TargetClaudeCode
	disclosedSkills[0].InstallName = "changed"
	disclosedHooks[0].Command = "changed"
	disclosedServers[0].Args[0] = "--changed"
	disclosedServers[0].Env["TOKEN"] = "CHANGED"
	disclosedScans[0].Status = "changed"
	disclosedSkipped[0].Reason = "changed"
	assertCandidateSetUnchanged(t, candidates)
}

func assertCandidateSetUnchanged(t *testing.T, candidates CandidateSet) {
	t.Helper()
	if got := candidates.Sources(); got[0].ResourceName != "instructions" || string(got[0].Content) != "instructions\n" {
		t.Fatalf("sources changed through alias: %#v", got)
	}
	if got := candidates.Skills(); got[0].InstallName != "review" || got[0].Targets[0] != targetpkg.TargetCodex {
		t.Fatalf("skills changed through alias: %#v", got)
	}
	if got := candidates.Hooks(); got[0].Command != "lint" {
		t.Fatalf("hooks changed through alias: %#v", got)
	}
	if got := candidates.MCPServers(); got[0].Command != "npx" || got[0].Args[0] != "-y" || got[0].Env["TOKEN"] != "TOKEN" {
		t.Fatalf("MCP servers changed through alias: %#v", got)
	}
	if got := candidates.Scans(); got[0].Status != "scanned" {
		t.Fatalf("scans changed through alias: %#v", got)
	}
	if got := candidates.Skipped(); got[0].Reason != "empty" {
		t.Fatalf("skipped observations changed through alias: %#v", got)
	}
}

func TestCandidateSetRejectsInvalidNestedFacts(t *testing.T) {
	if _, err := NewCandidateSet(
		[]Source{{ResourceName: "missing-content", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, LivePath: "live", SourcePath: "source"}},
		nil,
		nil,
		nil,
		nil,
		nil,
	); err == nil {
		t.Fatal("candidate set accepted a source without content")
	}
	if _, err := NewCandidateSet(
		nil,
		nil,
		nil,
		[]MCPServer{{ResourceName: "bad", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, LivePath: "live", Command: "npx", Env: map[string]string{"TOKEN": ""}}},
		nil,
		nil,
	); err == nil {
		t.Fatal("candidate set accepted an empty environment reference")
	}
	if _, err := NewCandidateSet(
		nil,
		nil,
		nil,
		nil,
		[]Scan{{ResourceName: "root", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, LivePath: "live", Status: "scanned", Entries: 1, Imported: 1, Skipped: 1}},
		nil,
	); err == nil {
		t.Fatal("candidate set accepted impossible scan counts")
	}

	for name, contentHash := range map[string]artifact.ContentHash{
		"empty":                 "",
		"unqualified":           "review",
		"short digest":          "sha256:review",
		"unsupported algorithm": artifact.ContentHash("sha512:" + strings.Repeat("0", 64)),
		"uppercase digest":      artifact.ContentHash("sha256:" + strings.Repeat("A", 64)),
		"leading whitespace":    artifact.ContentHash(" sha256:" + strings.Repeat("0", 64)),
		"control character":     artifact.ContentHash("sha256:" + strings.Repeat("0", 63) + "\n"),
	} {
		t.Run("skill content hash/"+name, func(t *testing.T) {
			if _, err := NewCandidateSet(
				nil,
				[]Skill{{
					ResourceName: "review",
					InstallName:  "review",
					Target:       targetpkg.TargetCodex,
					Targets:      []targetpkg.Target{targetpkg.TargetCodex},
					Scope:        targetpkg.ScopeProject,
					LivePath:     "/host/skills/review",
					ReadPath:     "/host/skills/review",
					SourcePath:   "/workspace/daem.d/skills/review",
					ContentHash:  contentHash,
				}},
				nil,
				nil,
				nil,
				nil,
			); err == nil {
				t.Fatalf("candidate set accepted malformed skill content hash %q", contentHash)
			}
		})
	}
}
