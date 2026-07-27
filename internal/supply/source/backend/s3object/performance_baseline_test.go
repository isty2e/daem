package s3object

import (
	"context"
	"fmt"
	"sync"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestReadPathVersionlessS3ReusesClientWithinResolverEpoch(t *testing.T) {
	fakeClient := &fakeS3Client{body: []byte("payload\n"), versionID: "resolved-version"}
	var factoryMu sync.Mutex
	factoryCalls := 0
	resolver, err := newResolverWithClientFactory(
		t.TempDir(),
		func(context.Context, clientConfiguration) (client, error) {
			factoryMu.Lock()
			factoryCalls++
			factoryMu.Unlock()
			return fakeClient, nil
		},
	)
	if err != nil {
		t.Fatalf("newResolverWithClientFactory returned error: %v", err)
	}

	const sourceCount = 4
	for index := range sourceCount {
		sourceSpec := sourcetest.S3(
			t,
			fmt.Sprintf("s3://daem/read-path-%d", index),
			"",
			"us-east-1",
			sourcepkg.S3ObjectFormatFile,
		)
		if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
			t.Fatalf("Resolve source[%d] returned error: %v", index, err)
		}
	}

	factoryMu.Lock()
	gotFactoryCalls := factoryCalls
	factoryMu.Unlock()
	if gotFactoryCalls != 1 || fakeClient.callCount() != sourceCount {
		t.Fatalf(
			"client factory/GetObject calls = %d/%d, want 1/%d",
			gotFactoryCalls,
			fakeClient.callCount(),
			sourceCount,
		)
	}
}

func TestReadPathBaselineWarmExactS3SkipsClientConstruction(t *testing.T) {
	fakeClient := &fakeS3Client{body: []byte("payload\n"), versionID: "version-1"}
	var factoryMu sync.Mutex
	factoryCalls := 0
	resolver, err := newResolverWithClientFactory(
		t.TempDir(),
		func(context.Context, clientConfiguration) (client, error) {
			factoryMu.Lock()
			factoryCalls++
			factoryMu.Unlock()
			return fakeClient, nil
		},
	)
	if err != nil {
		t.Fatalf("newResolverWithClientFactory returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/read-path",
		"version-1",
		"us-east-1",
		sourcepkg.S3ObjectFormatFile,
	)

	for index := range 2 {
		if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
			t.Fatalf("Resolve[%d] returned error: %v", index, err)
		}
	}

	factoryMu.Lock()
	gotFactoryCalls := factoryCalls
	factoryMu.Unlock()
	if gotFactoryCalls != 1 || fakeClient.callCount() != 1 {
		t.Fatalf(
			"client factory/GetObject calls after seed and warm hit = %d/%d, want 1/1",
			gotFactoryCalls,
			fakeClient.callCount(),
		)
	}
}

func BenchmarkS3ReadPath(b *testing.B) {
	b.Run("cold_exact", func(b *testing.B) {
		resolver, err := newResolverWithClientFactory(
			b.TempDir(),
			func(_ context.Context, _ clientConfiguration) (client, error) {
				return &fakeS3Client{
					body: []byte("payload\n"),
				}, nil
			},
		)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()

		for index := range b.N {
			sourceSpec := sourcetest.S3(
				b,
				"s3://daem/read-path",
				fmt.Sprintf("version-%d", index),
				"us-east-1",
				sourcepkg.S3ObjectFormatFile,
			)
			if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("warm_exact", func(b *testing.B) {
		fakeClient := &fakeS3Client{body: []byte("payload\n"), versionID: "version-1"}
		resolver, err := newResolverWithClient(b.TempDir(), fakeClient)
		if err != nil {
			b.Fatal(err)
		}
		sourceSpec := sourcetest.S3(
			b,
			"s3://daem/read-path",
			"version-1",
			"us-east-1",
			sourcepkg.S3ObjectFormatFile,
		)
		if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
				b.Fatal(err)
			}
		}
	})
}
