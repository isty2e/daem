package resolution

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolverDispatchesResolveBySourceKind(t *testing.T) {
	baseResolver := newFakeResolver(t)
	resolver := Resolver{
		local: fakeRootResolver{fakeResolver: baseResolver, name: "local"},
		git:   fakeRootResolver{fakeResolver: baseResolver, name: "git"},
		s3:    baseResolver,
	}

	for _, tt := range []struct {
		name       string
		sourceSpec source.Source
	}{
		{name: "local", sourceSpec: sourcetest.Local(t, "skills", source.LocalSourceModeVendor)},
		{name: "git", sourceSpec: mustGitSource(t, "https://example.test/repo.git", "skills", "main")},
		{name: "s3", sourceSpec: sourcetest.S3(t, "s3://bucket/key.tar.gz", "", "", source.S3ObjectFormatTarGzip)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolution, err := resolver.Resolve(context.Background(), tt.sourceSpec, noOperationOptions)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			wantID := mustSourceID(t, tt.sourceSpec)
			if resolution.Identity().SourceID() != wantID {
				t.Fatalf("SourceID = %q, want %q", resolution.Identity().SourceID(), wantID)
			}
		})
	}
}

func TestResolverRejectsUnsupportedSourceKind(t *testing.T) {
	_, err := (Resolver{}).Resolve(context.Background(), source.Source{}, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "unsupported source kind") {
		t.Fatalf("Resolve error = %v, want unsupported source kind", err)
	}
}

func TestResolverListsOnlyRootBackedSources(t *testing.T) {
	baseResolver := newFakeResolver(t)
	resolver := Resolver{
		local: fakeRootResolver{fakeResolver: baseResolver, name: "local"},
		git:   fakeRootResolver{fakeResolver: baseResolver, name: "git"},
		s3:    baseResolver,
	}

	for _, tt := range []struct {
		name       string
		sourceSpec source.Source
		wantPath   string
	}{
		{name: "local", sourceSpec: sourcetest.Local(t, "skills", source.LocalSourceModeVendor), wantPath: "local-root"},
		{name: "git", sourceSpec: mustGitSource(t, "https://example.test/repo.git", "skills", "main"), wantPath: "git-root"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listing, err := resolver.ListSourceRoot(context.Background(), tt.sourceSpec, noOperationOptions)
			if err != nil {
				t.Fatalf("ListSourceRoot returned error: %v", err)
			}
			if len(listing.ChildNames()) != 1 || listing.ChildNames()[0] != tt.wantPath {
				t.Fatalf("listing = %#v, want child %q", listing, tt.wantPath)
			}
		})
	}

	_, err := resolver.ListSourceRoot(context.Background(), sourcetest.S3(t, "s3://bucket/key.tar.gz", "", "", source.S3ObjectFormatTarGzip), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "S3 skill groups are unsupported") {
		t.Fatalf("ListSourceRoot S3 error = %v, want unsupported S3 skill group", err)
	}
}
