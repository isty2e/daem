package mutation

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestNormalizeDomainsWideAllocationGrowthIsSubquadratic(t *testing.T) {
	const (
		wideMultiplier            = 4
		smallDomainCount          = 128
		subquadraticGrowthCeiling = 8.0
		quadraticSensitivityFloor = 10.0
		allocationMeasurementRuns = 5
	)

	root := normalizePlatformPathKey(t.TempDir())
	store := mutationTestStore(t)
	small := normalizationScalingWideDomains(root, smallDomainCount)
	large := normalizationScalingWideDomains(root, smallDomainCount*wideMultiplier)

	optimizedSmall := normalizationScalingAllocations(t, allocationMeasurementRuns, func() ([]normalizedDomain, error) {
		return store.normalize(small)
	})
	optimizedLarge := normalizationScalingAllocations(t, allocationMeasurementRuns, func() ([]normalizedDomain, error) {
		return store.normalize(large)
	})
	referenceSmall := normalizationScalingAllocations(t, allocationMeasurementRuns, func() ([]normalizedDomain, error) {
		return store.normalizePairwiseReference(small)
	})
	referenceLarge := normalizationScalingAllocations(t, allocationMeasurementRuns, func() ([]normalizedDomain, error) {
		return store.normalizePairwiseReference(large)
	})

	optimizedRatio := optimizedLarge / optimizedSmall
	referenceRatio := referenceLarge / referenceSmall
	t.Logf(
		"%dx wide growth: optimized allocations %.0f -> %.0f (%.2fx); pairwise allocations %.0f -> %.0f (%.2fx)",
		wideMultiplier,
		optimizedSmall,
		optimizedLarge,
		optimizedRatio,
		referenceSmall,
		referenceLarge,
		referenceRatio,
	)
	if optimizedRatio >= subquadraticGrowthCeiling {
		t.Fatalf("optimized allocation growth = %.2fx, want below %.2fx", optimizedRatio, subquadraticGrowthCeiling)
	}
	if referenceRatio <= quadraticSensitivityFloor {
		t.Fatalf("pairwise negative-control allocation growth = %.2fx, want above %.2fx", referenceRatio, quadraticSensitivityFloor)
	}
}

func BenchmarkNormalizeDomainsWide(b *testing.B) {
	for _, domainCount := range []int{128, 512, 2048, 8192} {
		b.Run(fmt.Sprintf("domains=%d", domainCount), func(b *testing.B) {
			root := normalizePlatformPathKey(b.TempDir())
			benchmarkNormalizeDomains(b, normalizationScalingWideDomains(root, domainCount))
		})
	}
}

func BenchmarkNormalizeDomainsDeep(b *testing.B) {
	for _, domainCount := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("domains=%d", domainCount), func(b *testing.B) {
			root := normalizePlatformPathKey(b.TempDir())
			benchmarkNormalizeDomains(b, normalizationScalingDeepDomains(root, domainCount))
		})
	}
}

func BenchmarkNormalizeDomainsMixed(b *testing.B) {
	for _, distinctPathCount := range []int{128, 512, 2048} {
		b.Run(fmt.Sprintf("paths=%d", distinctPathCount), func(b *testing.B) {
			root := normalizePlatformPathKey(b.TempDir())
			benchmarkNormalizeDomains(b, normalizationScalingMixedDomains(root, distinctPathCount))
		})
	}
}

func benchmarkNormalizeDomains(b *testing.B, domains []Domain) {
	b.Helper()
	store, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	result, err := store.normalize(domains)
	if err != nil {
		b.Fatal(err)
	}
	pathFacts, ancestorSteps, pathBytes := normalizationScalingPathMetrics(domains)
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		result, err = store.normalize(domains)
		if err != nil {
			b.Fatal(err)
		}
	}
	if len(result) == 0 {
		b.Fatal("normalize returned no lease keys")
	}
	b.ReportMetric(float64(len(domains)), "domains/op")
	b.ReportMetric(float64(pathFacts), "path-facts/op")
	b.ReportMetric(float64(ancestorSteps), "ancestor-steps/op")
	if pathFacts > 0 {
		b.ReportMetric(float64(ancestorSteps)/float64(pathFacts), "ancestors/path")
	}
	b.ReportMetric(float64(pathBytes), "path-bytes/op")
	b.ReportMetric(float64(len(result)), "keys/op")
}

func normalizationScalingAllocations(
	t *testing.T,
	runs int,
	normalize func() ([]normalizedDomain, error),
) float64 {
	t.Helper()
	var result []normalizedDomain
	var resultErr error
	allocations := testing.AllocsPerRun(runs, func() {
		result, resultErr = normalize()
		if resultErr != nil {
			panic(resultErr)
		}
	})
	if len(result) == 0 {
		t.Fatal("normalize allocation measurement returned no lease keys")
	}
	return allocations
}

func normalizationScalingWideDomains(root string, count int) []Domain {
	domains := make([]Domain, 0, count)
	for index := range count {
		domains = append(domains, directMutationPathDomain(
			filepath.Join(root, "wide", fmt.Sprintf("sibling-%06d", index)),
			domainLogicalPath,
			AccessExclusive,
			"",
			"",
		))
	}
	return domains
}

func normalizationScalingDeepDomains(root string, count int) []Domain {
	domains := make([]Domain, 0, count)
	path := filepath.Join(root, "deep")
	for index := range count {
		path = filepath.Join(path, fmt.Sprintf("level-%03d", index))
		domains = append(domains, directMutationPathDomain(path, domainLogicalPath, AccessShared, "", ""))
	}
	return domains
}

func normalizationScalingMixedDomains(root string, distinctPathCount int) []Domain {
	domains := make([]Domain, 0, distinctPathCount*3+3)
	for index := range distinctPathCount {
		path := filepath.Join(root, "mixed", fmt.Sprintf("entry-%06d", index))
		domains = append(
			domains,
			directMutationPathDomain(path, domainLogicalPath, AccessShared, "", ""),
			directMutationPathDomain(path, domainLogicalPath, AccessExclusive, "", ""),
			directMutationPathDomain(path, domainPhysicalPath, AccessShared, "codex", "project"),
		)
	}
	domains = append(
		domains,
		Domain{kind: domainHostRoute, access: AccessExclusive, target: "codex", scope: "project", family: "plugin", containment: RouteContainmentCompletePaths},
		Domain{kind: domainHostRoute, access: AccessExclusive, target: "codex", scope: "project", family: "plugin", containment: RouteContainmentScope},
		Domain{kind: domainHostRoute, access: AccessExclusive, target: "codex", scope: "project", family: "plugin", containment: RouteContainmentUnknown},
	)
	return domains
}

func normalizationScalingPathMetrics(domains []Domain) (pathFacts int, ancestorSteps int, pathBytes int) {
	paths := make(map[string]struct{})
	for _, domain := range domains {
		if domain.kind != domainLogicalPath && domain.kind != domainPhysicalPath {
			continue
		}
		paths[domain.canonicalPath] = struct{}{}
	}
	for path := range paths {
		ancestorSteps += len(pathAncestors(path))
		pathBytes += len(path)
	}
	return len(paths), ancestorSteps, pathBytes
}
