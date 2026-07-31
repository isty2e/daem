package mutation

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNormalizeDomainsMatchesPairwiseReference(t *testing.T) {
	root := t.TempDir()
	store := mutationTestStore(t)
	pool := mutationNormalizationDomainPool(t, root)
	random := rand.New(rand.NewSource(20260713))

	for caseIndex := range 400 {
		domainCount := 1 + random.Intn(64)
		domains := make([]Domain, 0, domainCount)
		for range domainCount {
			domains = append(domains, pool[random.Intn(len(pool))])
		}

		want, wantErr := store.normalizePairwiseReference(domains)
		got, gotErr := store.normalize(domains)
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("case %d error = %v, want %v", caseIndex, gotErr, wantErr)
		}
		if gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Fatalf("case %d error = %q, want %q", caseIndex, gotErr, wantErr)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("case %d normalized = %#v, want %#v", caseIndex, got, want)
		}
	}
}

func TestNormalizeDomainsIsInputOrderInvariant(t *testing.T) {
	root := t.TempDir()
	store := mutationTestStore(t)
	pool := mutationNormalizationDomainPool(t, root)
	domains := []Domain{
		pool[0],
		pool[1],
		pool[5],
		pool[8],
		pool[len(pool)-3],
		pool[len(pool)-2],
		pool[len(pool)-1],
	}
	want, err := store.normalize(domains)
	if err != nil {
		t.Fatal(err)
	}

	random := rand.New(rand.NewSource(20260714))
	for iteration := range 200 {
		shuffled := slices.Clone(domains)
		random.Shuffle(len(shuffled), func(left int, right int) {
			shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
		})
		got, err := store.normalize(shuffled)
		if err != nil {
			t.Fatalf("iteration %d returned error: %v", iteration, err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("iteration %d normalized = %#v, want %#v", iteration, got, want)
		}
	}
}

func TestNormalizeDomainsAncestorSubsumptionIsComponentAndAccessAware(t *testing.T) {
	root := mustMutationTestCanonicalPath(t.TempDir()).keyPath
	store := mutationTestStore(t)
	exclusive := directMutationPathDomain(filepath.Join(root, "a"), domainLogicalPath, AccessExclusive, "", "")
	sharedMiddle := directMutationPathDomain(filepath.Join(root, "a", "middle"), domainLogicalPath, AccessShared, "", "")
	deepPhysical := directMutationPathDomain(filepath.Join(root, "a", "middle", "leaf"), domainPhysicalPath, AccessExclusive, "codex", "global")
	prefixSibling := directMutationPathDomain(filepath.Join(root, "a-b"), domainLogicalPath, AccessShared, "", "")

	normalized, err := store.normalize([]Domain{deepPhysical, prefixSibling, sharedMiddle, exclusive})
	if err != nil {
		t.Fatal(err)
	}
	byKey := normalizedMutationDomainsByKey(normalized)
	if _, present := byKey[pathKey(sharedMiddle.canonicalPath)]; present {
		t.Fatalf("shared middle path %q survived exclusive ancestor", sharedMiddle.canonicalPath)
	}
	if _, present := byKey[pathKey(deepPhysical.canonicalPath)]; present {
		t.Fatalf("deep path %q survived exclusive ancestor beyond shared middle", deepPhysical.canonicalPath)
	}
	if _, present := byKey[pathKey(prefixSibling.canonicalPath)]; !present {
		t.Fatalf("lexical-prefix sibling %q was incorrectly subsumed", prefixSibling.canonicalPath)
	}
	for _, key := range []string{
		encodedKey("host-target", "codex"),
		encodedKey("host-scope", "codex", "global"),
	} {
		if _, present := byKey[key]; !present {
			t.Fatalf("physical descendant host intent %q was lost", key)
		}
	}
}

func TestNormalizeDomainsNestedExclusiveAncestorsAreOrderIndependent(t *testing.T) {
	root := mustMutationTestCanonicalPath(t.TempDir()).keyPath
	store := mutationTestStore(t)
	top := directMutationPathDomain(filepath.Join(root, "top"), domainLogicalPath, AccessExclusive, "", "")
	middle := directMutationPathDomain(filepath.Join(root, "top", "middle"), domainLogicalPath, AccessExclusive, "", "")
	leaf := directMutationPathDomain(filepath.Join(root, "top", "middle", "leaf"), domainLogicalPath, AccessShared, "", "")
	sibling := directMutationPathDomain(filepath.Join(root, "sibling"), domainLogicalPath, AccessShared, "", "")
	domains := []Domain{top, middle, leaf, sibling}

	random := rand.New(rand.NewSource(20260715))
	for iteration := range 200 {
		shuffled := slices.Clone(domains)
		random.Shuffle(len(shuffled), func(left int, right int) {
			shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
		})
		normalized, err := store.normalize(shuffled)
		if err != nil {
			t.Fatalf("iteration %d returned error: %v", iteration, err)
		}
		byKey := normalizedMutationDomainsByKey(normalized)
		if _, present := byKey[pathKey(top.canonicalPath)]; !present {
			t.Fatalf("iteration %d lost highest exclusive ancestor", iteration)
		}
		for _, subsumed := range []Domain{middle, leaf} {
			if _, present := byKey[pathKey(subsumed.canonicalPath)]; present {
				t.Fatalf("iteration %d retained subsumed path %q", iteration, subsumed.canonicalPath)
			}
		}
		if _, present := byKey[pathKey(sibling.canonicalPath)]; !present {
			t.Fatalf("iteration %d lost unrelated sibling", iteration)
		}
	}
}

func TestNormalizeDomainsEmitsCompleteSharedAncestorUnionAcrossBranches(t *testing.T) {
	root := mustMutationTestCanonicalPath(t.TempDir()).keyPath
	store := mutationTestStore(t)
	left := directMutationPathDomain(filepath.Join(root, "tree", "left", "leaf"), domainLogicalPath, AccessShared, "", "")
	right := directMutationPathDomain(filepath.Join(root, "tree", "right", "leaf"), domainLogicalPath, AccessShared, "", "")

	normalized, err := store.normalize([]Domain{right, left})
	if err != nil {
		t.Fatal(err)
	}
	byKey := normalizedMutationDomainsByKey(normalized)
	wantPaths := map[string]struct{}{
		left.canonicalPath:  {},
		right.canonicalPath: {},
	}
	for _, domain := range []Domain{left, right} {
		for _, ancestor := range pathAncestors(domain.canonicalPath) {
			wantPaths[ancestor] = struct{}{}
		}
	}
	for path := range wantPaths {
		domain, present := byKey[pathKey(path)]
		if !present {
			t.Fatalf("shared ancestor union is missing %q", path)
		}
		if domain.access != AccessShared {
			t.Fatalf("shared ancestor %q access = %d, want shared", path, domain.access)
		}
	}
}

func TestNormalizeDomainsHandlesWideAndDeepPathSets(t *testing.T) {
	root := mustMutationTestCanonicalPath(t.TempDir()).keyPath
	store := mutationTestStore(t)

	const wideCount = 4096
	wide := make([]Domain, 0, wideCount)
	for index := range wideCount {
		wide = append(wide, directMutationPathDomain(
			filepath.Join(root, "wide", fmt.Sprintf("sibling-%04d", index)),
			domainLogicalPath,
			AccessExclusive,
			"",
			"",
		))
	}
	wideNormalized, err := store.normalize(wide)
	if err != nil {
		t.Fatal(err)
	}
	wideByKey := normalizedMutationDomainsByKey(wideNormalized)
	for _, domain := range wide {
		if _, present := wideByKey[pathKey(domain.canonicalPath)]; !present {
			t.Fatalf("wide sibling %q is missing", domain.canonicalPath)
		}
	}

	const deepCount = 256
	deep := make([]Domain, 0, deepCount)
	path := filepath.Join(root, "deep")
	for index := range deepCount {
		path = filepath.Join(path, fmt.Sprintf("level-%03d", index))
		deep = append(deep, directMutationPathDomain(path, domainLogicalPath, AccessShared, "", ""))
	}
	deepNormalized, err := store.normalize(deep)
	if err != nil {
		t.Fatal(err)
	}
	deepByKey := normalizedMutationDomainsByKey(deepNormalized)
	for _, domain := range deep {
		if _, present := deepByKey[pathKey(domain.canonicalPath)]; !present {
			t.Fatalf("shared deep path %q is missing", domain.canonicalPath)
		}
	}
}

func TestNormalizeDomainsCoalescesCanonicalAliasesBeforeSubsumption(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	real := mutationTestLogicalDomain(t, filepath.Join(realParent, "resource"), AccessShared)
	alias := mutationTestLogicalDomain(t, filepath.Join(aliasParent, "resource"), AccessExclusive)
	if real.canonicalPath != alias.canonicalPath {
		t.Fatalf("canonical paths differ: real=%q alias=%q", real.canonicalPath, alias.canonicalPath)
	}

	store := mutationTestStore(t)
	normalized, err := store.normalize([]Domain{real, alias})
	if err != nil {
		t.Fatal(err)
	}
	assertMutationTestDomainMode(t, normalized, pathKey(real.canonicalPath), AccessExclusive)
}

func mutationNormalizationDomainPool(t *testing.T, root string) []Domain {
	t.Helper()
	paths := []string{
		root,
		filepath.Join(root, "a"),
		filepath.Join(root, "a", "child"),
		filepath.Join(root, "a", "child", "grandchild"),
		filepath.Join(root, "a-b"),
		filepath.Join(root, "a-b", "child"),
		filepath.Join(root, "b"),
		filepath.Join(root, "b", "child"),
		filepath.Join(root, "c", "deep", "leaf"),
	}
	pool := make([]Domain, 0, len(paths)*4+3)
	for _, path := range paths {
		for _, access := range []AccessMode{AccessShared, AccessExclusive} {
			pool = append(pool, mutationTestLogicalDomain(t, path, access))
			physical, err := NewPhysicalPathDomain(PhysicalPathRequest{
				Path: path, Access: access, Effect: PathEffectDirectoryEntry,
				Target: "codex", Scope: "project",
			})
			if err != nil {
				t.Fatal(err)
			}
			pool = append(pool, physical)
		}
	}
	pool = append(
		pool,
		mutationTestRouteDomain(t, "codex", "project", "plugin", RouteContainmentCompletePaths),
		mutationTestRouteDomain(t, "codex", "project", "plugin", RouteContainmentScope),
		mutationTestRouteDomain(t, "codex", "project", "plugin", RouteContainmentUnknown),
	)
	return pool
}

func directMutationPathDomain(path string, kind domainKind, access AccessMode, target string, scope string) Domain {
	return Domain{
		kind:          kind,
		access:        access,
		canonicalPath: filepath.Clean(path),
		pathWitness:   "test-synthetic-v1:",
		requestedPath: filepath.Clean(path),
		effect:        PathEffectDirectoryEntry,
		target:        target,
		scope:         scope,
	}
}

func normalizedMutationDomainsByKey(domains []normalizedDomain) map[string]normalizedDomain {
	byKey := make(map[string]normalizedDomain, len(domains))
	for _, domain := range domains {
		byKey[domain.key] = domain
	}
	return byKey
}
