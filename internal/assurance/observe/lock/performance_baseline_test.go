package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	targetavailability "github.com/isty2e/daem/internal/target/availability"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestReadPathBaselineLockObservationResolveCounts(t *testing.T) {
	for _, test := range []struct {
		name      string
		resources int
	}{
		{name: "zero", resources: 0},
		{name: "small", resources: 1},
		{name: "multi", resources: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReadPathBaselineFixture(t, test.resources, false)
			counter := newReadPathCountingResolver(fixture.resolver)

			observations, err := lockObservationsWithResolver(
				context.Background(),
				counter,
				fixture.environment,
				fixture.locked,
				fixture.selection,
			)
			if err != nil {
				t.Fatalf("lockObservationsWithResolver returned error: %v", err)
			}
			if got := len(observations.ExactSupplies()); got != test.resources {
				t.Fatalf("exact Supply observations = %d, want %d", got, test.resources)
			}
			if counter.batchCalls != 1 || counter.requestCount != test.resources {
				t.Fatalf(
					"ResolveBatch calls/requests = %d/%d, want 1/%d",
					counter.batchCalls,
					counter.requestCount,
					test.resources,
				)
			}
		})
	}
}

func TestReadPathBaselineDuplicateSourceResolvesPerSubject(t *testing.T) {
	fixture := newReadPathBaselineFixture(t, 2, true)
	counter := newReadPathCountingResolver(fixture.resolver)

	observations, err := lockObservationsWithResolver(
		context.Background(),
		counter,
		fixture.environment,
		fixture.locked,
		fixture.selection,
	)
	if err != nil {
		t.Fatalf("lockObservationsWithResolver returned error: %v", err)
	}
	if got := len(observations.ExactSupplies()); got != 2 {
		t.Fatalf("exact Supply observations = %d, want 2", got)
	}
	if counter.batchCalls != 1 || counter.requestCount != 2 || len(counter.requestsBySource) != 1 {
		t.Fatalf(
			"ResolveBatch calls/requests/unique sources = %d/%d/%d, want 1/2/1",
			counter.batchCalls,
			counter.requestCount,
			len(counter.requestsBySource),
		)
	}
}

func BenchmarkLockObservationsWarmLocal(b *testing.B) {
	for _, resources := range []int{0, 1, 8} {
		b.Run(fmt.Sprintf("resources_%d", resources), func(b *testing.B) {
			fixture := newReadPathBaselineFixture(b, resources, false)
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if _, err := lockObservationsWithResolver(
					context.Background(),
					fixture.resolver,
					fixture.environment,
					fixture.locked,
					fixture.selection,
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type readPathBaselineFixture struct {
	resolver    acquisition.BatchResolver
	environment desired.Environment
	locked      lockmodel.File
	selection   targetselection.Selection
}

func newReadPathBaselineFixture(
	tb testing.TB,
	resourceCount int,
	duplicateSource bool,
) readPathBaselineFixture {
	tb.Helper()

	root := tb.TempDir()
	var declaration strings.Builder
	declaration.WriteString("version = 1\n")
	declaration.WriteString("targets = [\"codex\"]\n")
	for index := range resourceCount {
		name := fmt.Sprintf("item%d", index)
		sourcePath := fmt.Sprintf("skills/%s", name)
		if duplicateSource {
			sourcePath = "skills/shared"
		}
		writeReadPathBaselineFile(
			tb,
			root,
			filepath.Join(sourcePath, "SKILL.md"),
			fmt.Sprintf("---\nname: %s\ndescription: read-path baseline\n---\n", name),
		)
		fmt.Fprintf(
			&declaration,
			"\n[[skill]]\nname = %q\nsource = { path = %q, mode = \"vendor\" }\ntargets = [\"codex\"]\n",
			name,
			sourcePath,
		)
	}

	manifestPath := filepath.Join(root, "daem.toml")
	writeReadPathBaselineFile(tb, root, "daem.toml", declaration.String())
	environment, err := manifest.Decode([]byte(declaration.String()))
	if err != nil {
		tb.Fatalf("manifest.Decode returned error: %v", err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		tb.Fatalf("paths.Resolve returned error: %v", err)
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		tb.Fatalf("resolution.NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{})
	if err != nil {
		tb.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	selection, err := targetselection.ForAvailableTargets(
		targetavailability.FromEnvironment(environment),
		nil,
	)
	if err != nil {
		tb.Fatalf("targetselection.ForAvailableTargets returned error: %v", err)
	}
	return readPathBaselineFixture{
		resolver:    resolver,
		environment: environment,
		locked:      locked,
		selection:   selection,
	}
}

func writeReadPathBaselineFile(tb testing.TB, root string, relativePath string, content string) {
	tb.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		tb.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatalf("WriteFile returned error: %v", err)
	}
}

type readPathCountingResolver struct {
	inner            acquisition.BatchResolver
	batchCalls       int
	requestCount     int
	requestsBySource map[artifact.SourceID]int
}

func newReadPathCountingResolver(inner acquisition.BatchResolver) *readPathCountingResolver {
	return &readPathCountingResolver{
		inner:            inner,
		requestsBySource: make(map[artifact.SourceID]int),
	}
}

func (resolver *readPathCountingResolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	resolver.batchCalls++
	resolver.requestCount += len(requests)
	for _, request := range requests {
		sourceID, err := source.SourceIDFor(request.Source())
		if err != nil {
			return nil, err
		}
		resolver.requestsBySource[sourceID]++
	}
	return resolver.inner.ResolveBatch(ctx, requests, options)
}
