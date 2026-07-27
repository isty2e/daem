package resolution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func BenchmarkDuplicateGitReadPath(b *testing.B) {
	if _, err := exec.LookPath("git"); err != nil {
		b.Skip("git executable is required")
	}

	root := b.TempDir()
	repositoryPath := filepath.Join(root, "repository")
	runReadPathBenchmarkGit(b, "", "init", repositoryPath)
	runReadPathBenchmarkGit(b, repositoryPath, "checkout", "-b", "main")
	runReadPathBenchmarkGit(b, repositoryPath, "config", "user.email", "daem@example.invalid")
	runReadPathBenchmarkGit(b, repositoryPath, "config", "user.name", "Agent Env Benchmark")
	sourcePath := filepath.Join(repositoryPath, "instructions", "shared.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("shared instructions\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	runReadPathBenchmarkGit(b, repositoryPath, "add", ".")
	runReadPathBenchmarkGit(b, repositoryPath, "commit", "-m", "add shared instructions")
	commit := runReadPathBenchmarkGit(b, repositoryPath, "rev-parse", "HEAD")

	sourceSpec, err := sourcepkg.NewGitSource(repositoryPath, "instructions/shared.md", commit)
	if err != nil {
		b.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "project", "daem.toml"))
	if err != nil {
		b.Fatal(err)
	}
	resolver, err := NewResolver(paths)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		b.Fatalf("seed Resolve returned error: %v", err)
	}

	const requestCount = 8
	requests := make([]acquisition.Request, 0, requestCount)
	for index := range requestCount {
		request, err := acquisition.NewRequest(
			acquisition.RequestID(fmt.Sprintf("baseline:%06d", index)),
			index,
			acquisition.OperationResolve,
			sourceSpec,
		)
		if err != nil {
			b.Fatal(err)
		}
		requests = append(requests, request)
	}

	b.Run("sequential_resolve", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for range requestCount {
				if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("batch_resolve", func(b *testing.B) {
		options := acquisition.NewBatchOptions(requestCount, nil)
		b.ReportAllocs()
		for range b.N {
			results, err := resolver.ResolveBatch(context.Background(), requests, options)
			if err != nil {
				b.Fatal(err)
			}
			if len(results) != requestCount {
				b.Fatalf("ResolveBatch results = %d, want %d", len(results), requestCount)
			}
		}
	})
}

func runReadPathBenchmarkGit(tb testing.TB, directory string, arguments ...string) string {
	tb.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		tb.Fatalf("git %v returned error: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}
