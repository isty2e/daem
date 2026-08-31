package operationplan

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestCompileAdoptPreservesDomainOrderAndRevisionRoles(t *testing.T) {
	t.Parallel()

	barrierRevision := mutation.NewBoundedContentRevisionRequest(
		"/barrier",
		mutation.PathEffectDirectoryEntry,
	)
	program := CompileAdopt(AdoptInput{
		BarrierRevisions:        []mutation.RevisionRequest{barrierRevision},
		OutputPath:              "/out",
		OutputMaximumBytes:      4096,
		SelectorLockfilePath:    "/lock",
		MetadataTransactionPath: "/metadata",
		Sources: []AdoptSource{{
			SourcePath: "/source-stage",
			LivePath:   "/source-live",
			Target:     "codex",
			Scope:      "project",
		}},
		SkillSourcePaths: []string{"/skill-stage"},
		SkillRoutes: []AdoptSkillRoute{{
			LivePath: "/skill-live",
			ReadPath: "/skill-read",
			Target:   "claude-code",
			Scope:    "global",
		}},
		Hooks: []AdoptPhysicalPath{{
			Path:   "/hook",
			Target: "claude-code",
			Scope:  "project",
		}},
		MCPSources: []AdoptMCPSource{{
			PrimaryPath:         "/mcp-primary",
			Target:              "codex",
			Scope:               "project",
			RequiredAbsentPaths: []string{"/mcp-alternate"},
		}},
		Scans: []AdoptScan{
			{
				Path:         "/scan-file",
				Target:       "opencode",
				Scope:        "global",
				Kind:         AdoptScanBoundedFile,
				MaximumBytes: 2048,
			},
			{
				Path:   "/scan-directory",
				Target: "antigravity",
				Scope:  "project",
				Kind:   AdoptScanDirectoryListing,
			},
		},
	})
	domains, revisions, err := evaluateAdoptProgram(program)
	if err != nil {
		t.Fatal(err)
	}

	wantDomains := []string{
		"logical:exclusive:1:/out",
		"logical:shared:2:/out",
		"logical:shared:1:/lock",
		"logical:shared:2:/lock",
		"logical:exclusive:1:/metadata",
		"logical:exclusive:1:/source-stage",
		"physical:shared:2:codex:project:/source-live",
		"logical:exclusive:1:/skill-stage",
		"physical:shared:1:claude-code:global:/skill-live",
		"physical:shared:2:claude-code:global:/skill-read",
		"physical:shared:2:claude-code:project:/hook",
		"physical:shared:2:codex:project:/mcp-primary",
		"physical:shared:1:codex:project:/mcp-alternate",
		"physical:shared:1:opencode:global:/scan-file",
		"physical:shared:2:opencode:global:/scan-file",
		"physical:shared:2:antigravity:project:/scan-directory",
	}
	if got := adoptDomainKeys(domains); !slices.Equal(got, wantDomains) {
		t.Fatalf("domain requests = %#v, want %#v", got, wantDomains)
	}

	full := revisions.Revisions()
	stable := revisions.StableRevisions()
	for _, path := range []string{
		"/barrier",
		"/out",
		"/metadata",
		"/source-stage",
		"/source-live",
		"/skill-stage",
		"/skill-live",
		"/skill-read",
		"/hook",
		"/mcp-alternate",
		"/scan-file",
		"/scan-directory",
	} {
		if !containsAdoptRevisionPath(full, path) {
			t.Fatalf("full revisions omit %q: %#v", path, full)
		}
	}
	for _, path := range []string{"/source-stage", "/skill-stage", "/mcp-primary"} {
		if containsAdoptRevisionPath(stable, path) {
			t.Fatalf("stable revisions unexpectedly include %q: %#v", path, stable)
		}
	}
	if containsAdoptRevisionPath(full, "/mcp-primary") {
		t.Fatalf("full revisions unexpectedly include externally validated MCP primary: %#v", full)
	}
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		expected, requestErr := mutation.NewBoundedFileRevisionRequest(4096, "/out", effect)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if !containsExactAdoptRevision(stable, expected) {
			t.Fatalf("stable revisions omit exact bounded output request: %#v", stable)
		}
	}
	if !containsExactAdoptRevision(stable, mutation.NewRequiredAbsentRevisionRequest("/mcp-alternate")) {
		t.Fatalf("stable revisions omit exact required-absent request: %#v", stable)
	}
	if !containsExactAdoptRevision(stable, mutation.NewBoundedDirectoryListingRevisionRequest("/scan-directory")) {
		t.Fatalf("stable revisions omit exact directory-listing request: %#v", stable)
	}
	assertAdoptRevisionSort(t, full)
	assertAdoptRevisionSort(t, stable)
}

func TestCompileAdoptDeduplicatesPhysicalDomainsAndLetsAuthoritativeEvidenceReplaceObservedEvidence(t *testing.T) {
	t.Parallel()

	program := CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
		Hooks: []AdoptPhysicalPath{
			{Path: "/shared", Target: "codex", Scope: "project"},
			{Path: "/shared", Target: "codex", Scope: "project"},
			{Path: "/shared", Target: "claude-code", Scope: "global"},
		},
		Scans: []AdoptScan{{
			Path: "/shared", Target: "codex", Scope: "project", Kind: AdoptScanDirectoryListing,
		}},
	})
	domains, revisions, err := evaluateAdoptProgram(program)
	if err != nil {
		t.Fatal(err)
	}

	counts := make(map[string]int)
	for _, key := range adoptDomainKeys(domains) {
		counts[key]++
	}
	for _, key := range []string{
		"physical:shared:2:codex:project:/shared",
		"physical:shared:2:claude-code:global:/shared",
	} {
		if counts[key] != 1 {
			t.Fatalf("physical referent domain %q count = %d, want 1", key, counts[key])
		}
	}
	if !containsExactAdoptRevision(
		revisions.Revisions(),
		mutation.NewBoundedDirectoryListingRevisionRequest("/shared"),
	) {
		t.Fatalf(
			"authoritative directory-listing evidence did not replace content evidence: %#v",
			revisions.Revisions(),
		)
	}
}

func TestCompileAdoptRejectsConflictingAuthoritativeEvidence(t *testing.T) {
	t.Parallel()

	_, _, err := evaluateAdoptProgram(CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
		MCPSources: []AdoptMCPSource{{
			PrimaryPath: "/primary", Target: "codex", Scope: "project",
			RequiredAbsentPaths: []string{"/shared"},
		}},
		Scans: []AdoptScan{{
			Path: "/shared", Target: "codex", Scope: "project",
			Kind: AdoptScanBoundedFile, MaximumBytes: 1024,
		}},
	}))
	if err == nil {
		t.Fatal("CompileAdopt accepted conflicting authoritative revision semantics")
	}
}

func TestCompileAdoptRejectsUnknownScanEvidenceAtItsOrderedStep(t *testing.T) {
	t.Parallel()

	program := CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
		Scans: []AdoptScan{{
			Path: "/scan", Target: "codex", Scope: "project", Kind: AdoptScanKind("unknown"),
		}},
	})
	steps := program.Steps()
	if len(steps) == 0 || steps[len(steps)-1].Preflight() == nil {
		t.Fatal("CompileAdopt omitted the ordered unknown-scan error step")
	}
	if _, _, err := evaluateAdoptProgram(program); err == nil {
		t.Fatal("CompileAdopt evaluation accepted an unknown scan evidence kind")
	}
}

func TestCompileAdoptExternalValidationRemovesSameKeyScanRevision(t *testing.T) {
	t.Parallel()

	_, revisions, err := evaluateAdoptProgram(CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
		MCPSources: []AdoptMCPSource{{
			PrimaryPath: "/shared", Target: "codex", Scope: "project",
		}},
		Scans: []AdoptScan{{
			Path: "/shared", Target: "codex", Scope: "project", Kind: AdoptScanDirectoryListing,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if containsAdoptRevisionPath(revisions.Revisions(), "/shared") ||
		containsAdoptRevisionPath(revisions.StableRevisions(), "/shared") {
		t.Fatalf(
			"externally validated path retained a generic revision: full=%#v stable=%#v",
			revisions.Revisions(),
			revisions.StableRevisions(),
		)
	}
}

func TestAdoptProgramAndRevisionPlanReturnDefensiveCopies(t *testing.T) {
	t.Parallel()

	program := CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
	})
	steps := program.Steps()
	steps[0] = AdoptStep{}
	if _, ok := program.Steps()[0].Domain(); !ok {
		t.Fatal("step mutation escaped program ownership")
	}

	_, revisions, err := evaluateAdoptProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	full := revisions.Revisions()
	stable := revisions.StableRevisions()
	full[0] = mutation.RevisionRequest{}
	stable[0] = mutation.RevisionRequest{}
	if revisions.Revisions()[0].Path == "" || revisions.StableRevisions()[0].Path == "" {
		t.Fatal("revision mutation escaped plan ownership")
	}
}

func TestAdoptRevisionCompilerRejectsOutOfOrderAndIncompleteConsumption(t *testing.T) {
	t.Parallel()

	program := CompileAdopt(AdoptInput{
		OutputPath:              "/out",
		OutputMaximumBytes:      1024,
		MetadataTransactionPath: "/metadata",
	})
	steps := program.Steps()
	compiler, err := program.NewRevisionCompiler()
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.ApplyAfterDomain(steps[1]); err == nil {
		t.Fatal("revision compiler accepted an out-of-order step")
	}
	if _, err := compiler.Compile(); err == nil {
		t.Fatal("revision compiler accepted incomplete step consumption")
	}
}

func evaluateAdoptProgram(
	program AdoptProgram,
) ([]AdoptDomainRequest, AdoptRevisionPlan, error) {
	compiler, err := program.NewRevisionCompiler()
	if err != nil {
		return nil, AdoptRevisionPlan{}, err
	}
	domains := make([]AdoptDomainRequest, 0)
	for _, step := range program.Steps() {
		if err := step.Preflight(); err != nil {
			return nil, AdoptRevisionPlan{}, err
		}
		if domain, ok := step.Domain(); ok {
			domains = append(domains, domain)
		}
		if err := compiler.ApplyAfterDomain(step); err != nil {
			return nil, AdoptRevisionPlan{}, err
		}
	}
	revisions, err := compiler.Compile()
	if err != nil {
		return nil, AdoptRevisionPlan{}, err
	}
	return domains, revisions, nil
}

func adoptDomainKeys(requests []AdoptDomainRequest) []string {
	keys := make([]string, 0, len(requests))
	for _, request := range requests {
		if logical, ok := request.Logical(); ok {
			keys = append(keys, "logical:"+accessString(logical.Access)+":"+effectString(logical.Effect)+":"+logical.Path)
			continue
		}
		physical, ok := request.Physical()
		if !ok {
			keys = append(keys, "invalid")
			continue
		}
		keys = append(keys, "physical:"+accessString(physical.Access)+":"+effectString(physical.Effect)+":"+physical.Target+":"+physical.Scope+":"+physical.Path)
	}
	return keys
}

func accessString(access mutation.AccessMode) string {
	switch access {
	case mutation.AccessShared:
		return "shared"
	case mutation.AccessExclusive:
		return "exclusive"
	default:
		return "invalid"
	}
}

func effectString(effect mutation.PathEffect) string {
	switch effect {
	case mutation.PathEffectDirectoryEntry:
		return "1"
	case mutation.PathEffectReferent:
		return "2"
	default:
		return "invalid"
	}
}

func containsAdoptRevisionPath(requests []mutation.RevisionRequest, path string) bool {
	for _, request := range requests {
		if request.Path == path {
			return true
		}
	}
	return false
}

func containsExactAdoptRevision(requests []mutation.RevisionRequest, expected mutation.RevisionRequest) bool {
	for _, request := range requests {
		if request.Equal(expected) {
			return true
		}
	}
	return false
}

func assertAdoptRevisionSort(t *testing.T, requests []mutation.RevisionRequest) {
	t.Helper()
	for index := 1; index < len(requests); index++ {
		left := effectString(requests[index-1].Effect) + ":" + requests[index-1].Path
		right := effectString(requests[index].Effect) + ":" + requests[index].Path
		if left > right {
			t.Fatalf("revision requests are not sorted: %q before %q", left, right)
		}
	}
}
