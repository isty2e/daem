package adopt

import (
	"path/filepath"
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
	serverRequiredAbsentPaths := []string{".codex/config.jsonc"}
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
		SourceRoutes: []SkillSourceRoute{{
			Target: targetpkg.TargetCodex, LivePath: "/host/skills/review", ReadPath: "/host/skills/review",
		}},
		SourcePath:  "/workspace/daem.d/skills/review",
		ContentHash: artifact.HashFileContent([]byte("review")),
	}}
	servers := []MCPServer{{
		ResourceName: "context7",
		Target:       targetpkg.TargetCodex,
		Scope:        targetpkg.ScopeProject,
		SourceRoute: MCPSourceRoute{
			PrimaryPath:         ".codex/config.toml",
			PrimaryRevision:     "test-source-revision",
			MaximumBytes:        1024,
			ContentPath:         "/mcp_servers/context7",
			RequiredAbsentPaths: serverRequiredAbsentPaths,
		},
		Command: "npx",
		Args:    serverArgs,
		Env:     serverEnv,
	}}
	candidates, err := NewCandidateSet(CandidateSetInput{
		Sources: sources,
		Skills:  skills,
		Hooks: []Hook{{
			ResourceName: "lint",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     ".codex/hooks.json",
			Event:        "PreToolUse",
			Command:      "lint",
		}},
		MCPServers: servers,
		Scans: []Scan{{
			ResourceKind: "skill",
			ResourceName: "skill-root",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     "/host/skills",
			Status:       "scanned",
			Entries:      1,
			Imported:     1,
		}},
		Skipped: []Skipped{{LivePath: "/host/empty", Reason: "empty"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	sourceContent[0] = 'X'
	skillTargets[0] = targetpkg.TargetClaudeCode
	serverArgs[0] = "--changed"
	serverEnv["TOKEN"] = "CHANGED"
	serverRequiredAbsentPaths[0] = "changed"
	sources[0].ResourceName = "changed"
	skills[0].InstallName = "changed"
	skills[0].SourceRoutes[0].ReadPath = "/changed"
	servers[0].Command = "changed"
	assertCandidateSetUnchanged(t, candidates)

	disclosedSources := candidates.Sources()
	disclosedSkills := candidates.Skills()
	disclosedHooks := candidates.Hooks()
	disclosedServers := candidates.MCPServers()
	disclosedAuthorities := candidates.MCPSourceAuthorities()
	disclosedScans := candidates.Scans()
	disclosedSkipped := candidates.Skipped()
	disclosedSources[0].Content[0] = 'Y'
	disclosedSources[0].ResourceName = "changed"
	disclosedSkills[0].Targets[0] = targetpkg.TargetClaudeCode
	disclosedSkills[0].InstallName = "changed"
	disclosedSkills[0].SourceRoutes[0].ReadPath = "/changed"
	disclosedHooks[0].Command = "changed"
	disclosedServers[0].Args[0] = "--changed"
	disclosedServers[0].Env["TOKEN"] = "CHANGED"
	disclosedServers[0].SourceRoute.RequiredAbsentPaths[0] = "changed"
	disclosedAuthorities[0].Route.RequiredAbsentPaths[0] = "changed"
	disclosedScans[0].Status = "changed"
	disclosedSkipped[0].Reason = "changed"
	assertCandidateSetUnchanged(t, candidates)
}

func assertCandidateSetUnchanged(t *testing.T, candidates CandidateSet) {
	t.Helper()
	if got := candidates.Sources(); got[0].ResourceName != "instructions" || string(got[0].Content) != "instructions\n" {
		t.Fatalf("sources changed through alias: %#v", got)
	}
	if got := candidates.Skills(); got[0].InstallName != "review" || got[0].Targets[0] != targetpkg.TargetCodex ||
		got[0].SourceRoutes[0].ReadPath != "/host/skills/review" {
		t.Fatalf("skills changed through alias: %#v", got)
	}
	if got := candidates.Hooks(); got[0].Command != "lint" {
		t.Fatalf("hooks changed through alias: %#v", got)
	}
	if got := candidates.MCPServers(); got[0].Command != "npx" || got[0].Args[0] != "-y" || got[0].Env["TOKEN"] != "TOKEN" ||
		got[0].SourceRoute.RequiredAbsentPaths[0] != ".codex/config.jsonc" {
		t.Fatalf("MCP servers changed through alias: %#v", got)
	}
	if got := candidates.MCPSourceAuthorities(); got[0].Route.RequiredAbsentPaths[0] != ".codex/config.jsonc" {
		t.Fatalf("MCP source authorities changed through alias: %#v", got)
	}
	if got := candidates.Scans(); got[0].Status != "scanned" {
		t.Fatalf("scans changed through alias: %#v", got)
	}
	if got := candidates.Skipped(); got[0].Reason != "empty" {
		t.Fatalf("skipped observations changed through alias: %#v", got)
	}
}

func TestCandidateSetRejectsInvalidNestedFacts(t *testing.T) {
	if _, err := NewCandidateSet(CandidateSetInput{
		Sources: []Source{{ResourceName: "missing-content", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, LivePath: "live", SourcePath: "source"}},
	}); err == nil {
		t.Fatal("candidate set accepted a source without content")
	}
	if _, err := NewCandidateSet(CandidateSetInput{
		MCPServers: []MCPServer{{
			ResourceName: "bad",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  testMCPSourceRoute(t, "live", "/mcp/bad"),
			Command:      "npx",
			Env:          map[string]string{"TOKEN": ""},
		}},
	}); err == nil {
		t.Fatal("candidate set accepted an empty environment reference")
	}
	if _, err := NewCandidateSet(CandidateSetInput{
		Scans: []Scan{{ResourceKind: "skill", ResourceName: "root", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, LivePath: "live", Status: "scanned", Entries: 1, Imported: 1, Skipped: 1}},
	}); err == nil {
		t.Fatal("candidate set accepted impossible scan counts")
	}
	if _, err := NewCandidateSet(CandidateSetInput{
		Skills: []Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       targetpkg.TargetCodex,
			Targets:      []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetClaudeCode},
			Scope:        targetpkg.ScopeProject,
			SourceRoutes: []SkillSourceRoute{{
				Target: targetpkg.TargetClaudeCode, LivePath: "/claude/skills/review", ReadPath: "/claude/skills/review",
			}},
			SourcePath:  "/workspace/daem.d/skills/review",
			ContentHash: artifact.HashFileContent([]byte("review")),
		}},
	}); err == nil || !strings.Contains(err.Error(), "representative target") {
		t.Fatalf("candidate set missing representative source route error = %v", err)
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
			if _, err := NewCandidateSet(CandidateSetInput{
				Skills: []Skill{{
					ResourceName: "review",
					InstallName:  "review",
					Target:       targetpkg.TargetCodex,
					Targets:      []targetpkg.Target{targetpkg.TargetCodex},
					Scope:        targetpkg.ScopeProject,
					SourceRoutes: []SkillSourceRoute{{
						Target: targetpkg.TargetCodex, LivePath: "/host/skills/review", ReadPath: "/host/skills/review",
					}},
					SourcePath:  "/workspace/daem.d/skills/review",
					ContentHash: contentHash,
				}},
			}); err == nil {
				t.Fatalf("candidate set accepted malformed skill content hash %q", contentHash)
			}
		})
	}
}

func TestCandidateSetRejectsDuplicateMCPProjectionSubject(t *testing.T) {
	servers := []MCPServer{
		{
			ResourceName: "context7",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  testMCPSourceRoute(t, "project-config", "/mcp/context7"),
			Command:      "npx",
		},
		{
			ResourceName: "context7",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  testMCPSourceRoute(t, "other-project-config", "/mcp/context7"),
			Command:      "node",
		},
	}

	_, err := NewCandidateSet(CandidateSetInput{MCPServers: servers})
	if err == nil || !strings.Contains(err.Error(), "duplicate imported mcp_server subject") {
		t.Fatalf("NewCandidateSet error = %v, want duplicate projection subject", err)
	}
}

func TestCandidateSetRejectsConflictingMCPSourceAuthorityForOneSubject(t *testing.T) {
	route := testMCPSourceRoute(t, "project-config", "/mcp/context7")
	conflictingRoute := route
	conflictingRoute.PrimaryRevision = "other-source-revision"

	_, err := NewCandidateSet(CandidateSetInput{
		MCPSourceAuthorities: []MCPSourceAuthority{
			{Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, Route: route},
			{Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, Route: conflictingRoute},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting exact revisions") {
		t.Fatalf("NewCandidateSet error = %v, want conflicting source authority", err)
	}
}

func TestCandidateSetRejectsConflictingMCPSourceAuthorityForOnePhysicalFile(t *testing.T) {
	left := testMCPSourceRoute(t, "project-config", "/mcp/context7")
	right := testMCPSourceRoute(t, "project-config", "/mcp/other")
	right.PrimaryRevision = "other-source-revision"

	_, err := NewCandidateSet(CandidateSetInput{
		MCPSourceAuthorities: []MCPSourceAuthority{
			{Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, Route: left},
			{Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject, Route: right},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "primary source") {
		t.Fatalf("NewCandidateSet error = %v, want conflicting physical source authority", err)
	}
}

func testMCPSourceRoute(t *testing.T, primaryPath string, contentPath string) MCPSourceRoute {
	t.Helper()
	route, err := NewMCPSourceRoute(MCPSourceRouteInput{
		PrimaryPath:     primaryPath,
		PrimaryRevision: "test-source-revision",
		MaximumBytes:    1024,
		ContentPath:     contentPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func TestNewMCPSourceRouteCanonicalizesAndValidatesAbsencePreconditions(t *testing.T) {
	route, err := NewMCPSourceRoute(MCPSourceRouteInput{
		PrimaryPath:         "opencode.json",
		PrimaryRevision:     "test-source-revision",
		MaximumBytes:        1024,
		ContentPath:         "/mcp/context7",
		RequiredAbsentPaths: []string{"z.jsonc", "a.jsonc", "z.jsonc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(route.RequiredAbsentPaths) != 2 ||
		route.RequiredAbsentPaths[0] != "a.jsonc" ||
		route.RequiredAbsentPaths[1] != "z.jsonc" {
		t.Fatalf("required-absent paths = %#v, want sorted unique paths", route.RequiredAbsentPaths)
	}
	if _, err := NewMCPSourceRoute(MCPSourceRouteInput{
		PrimaryPath:         "opencode.json",
		PrimaryRevision:     "test-source-revision",
		MaximumBytes:        1024,
		ContentPath:         "/mcp/context7",
		RequiredAbsentPaths: []string{"opencode.json"},
	}); err == nil {
		t.Fatal("source route accepted the primary config as required absent")
	}
}

func TestCandidateSetRejectsConflictingSkillIdentitiesAtOneSourcePath(t *testing.T) {
	base := Skill{
		ResourceName: "review",
		InstallName:  "review",
		Target:       targetpkg.TargetCodex,
		Targets:      []targetpkg.Target{targetpkg.TargetCodex},
		Scope:        targetpkg.ScopeProject,
		SourceRoutes: []SkillSourceRoute{{
			Target: targetpkg.TargetCodex, LivePath: "/host/skills/review", ReadPath: "/host/skills/review",
		}},
		SourcePath:  "/workspace/daem.d/skills/review",
		ContentHash: artifact.HashFileContent([]byte("first")),
	}
	conflicting := base
	conflicting.Target = targetpkg.TargetOpenCode
	conflicting.Targets = []targetpkg.Target{targetpkg.TargetOpenCode}
	conflicting.SourceRoutes = []SkillSourceRoute{{
		Target: targetpkg.TargetOpenCode, LivePath: "/host/skills/review", ReadPath: "/host/skills/review",
	}}
	conflicting.ContentHash = artifact.HashFileContent([]byte("second"))

	if _, err := NewCandidateSet(CandidateSetInput{Skills: []Skill{base, conflicting}}); err == nil ||
		!strings.Contains(err.Error(), "conflicting content identities") {
		t.Fatalf("NewCandidateSet error = %v, want source identity conflict", err)
	}
}

func TestSkillExpectedSourceIdentityRetainsResolvedLocalProvenance(t *testing.T) {
	readPath := filepath.Join(string(filepath.Separator), "host", "skills", "review")
	skill := Skill{
		Target: targetpkg.TargetCodex,
		SourceRoutes: []SkillSourceRoute{{
			Target: targetpkg.TargetCodex, LivePath: readPath, ReadPath: readPath,
		}},
		ContentHash: artifact.HashFileContent([]byte("review")),
	}
	identity, err := skill.ExpectedSourceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wantSourceID := artifact.SourceID("local:" + filepath.ToSlash(readPath) + "?mode=vendor")
	if identity.SourceID() != wantSourceID || identity.Kind() != artifact.ArtifactKindDirectory ||
		identity.ContentHash() != skill.ContentHash {
		t.Fatalf("source identity = (%q, %q, %q), want (%q, %q, %q)",
			identity.SourceID(), identity.Kind(), identity.ContentHash(),
			wantSourceID, artifact.ArtifactKindDirectory, skill.ContentHash)
	}
}

func TestSkillExpectedSourceIdentityRejectsUnresolvedReadPath(t *testing.T) {
	root := t.TempDir()
	for _, readPath := range []string{
		"",
		"relative/skill",
		filepath.Join(root, "nested") + string(filepath.Separator) + ".." + string(filepath.Separator) + "skill",
	} {
		skill := Skill{
			Target: targetpkg.TargetCodex,
			SourceRoutes: []SkillSourceRoute{{
				Target: targetpkg.TargetCodex, LivePath: readPath, ReadPath: readPath,
			}},
			ContentHash: artifact.HashFileContent([]byte("review")),
		}
		if _, err := skill.ExpectedSourceIdentity(); err == nil ||
			!strings.Contains(err.Error(), "canonical and absolute") {
			t.Fatalf("ExpectedSourceIdentity(%q) error = %v, want canonical absolute path rejection", readPath, err)
		}
	}
}
