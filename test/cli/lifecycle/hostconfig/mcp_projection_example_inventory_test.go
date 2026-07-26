package cli_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

const mcpPublicExampleInventoryMarker = "Minimal manifests are available at"

var mcpPublicExampleLinkPattern = regexp.MustCompile(`\]\(\.\./examples/([^)]+)\)`)

func assertMCPPublicExampleInventory(t *testing.T, cases []mcpPublicExampleCase) {
	t.Helper()
	linked, err := parseMCPPublicExampleLinks(string(readRepoFile(t, "docs", "manifest.md")))
	if err != nil {
		t.Fatal(err)
	}
	physical := physicalMCPPublicExamples(t)
	if err := validateMCPPublicExampleInventory(linked, physical, cases); err != nil {
		t.Fatal(err)
	}
}

func parseMCPPublicExampleLinks(content string) ([]string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if count := strings.Count(content, mcpPublicExampleInventoryMarker); count != 1 {
		return nil, fmt.Errorf("docs/manifest.md MCP example inventory marker count = %d, want one", count)
	}
	start := strings.Index(content, mcpPublicExampleInventoryMarker)
	inventory := content[start:]
	if end := strings.Index(inventory, "\n\n"); end >= 0 {
		inventory = inventory[:end]
	}
	matches := mcpPublicExampleLinkPattern.FindAllStringSubmatch(inventory, -1)
	result := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		link := strings.TrimSpace(match[1])
		if path.Base(link) != link || path.Ext(link) != ".toml" {
			return nil, fmt.Errorf("MCP public example link %q must name one direct TOML example", link)
		}
		if _, duplicate := seen[link]; duplicate {
			return nil, fmt.Errorf("duplicate MCP public example link %q", link)
		}
		seen[link] = struct{}{}
		result = append(result, link)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("docs/manifest.md contains no linked MCP public examples")
	}
	return result, nil
}

func physicalMCPPublicExamples(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(testkit.RepositoryRoot(t), "examples"))
	if err != nil {
		t.Fatalf("read examples directory: %v", err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".toml" || !strings.Contains(strings.ToLower(name), "mcp") {
			continue
		}
		result = append(result, name)
	}
	return result
}

func validateMCPPublicExampleInventory(
	linked []string,
	physical []string,
	cases []mcpPublicExampleCase,
) error {
	tabled := make([]string, 0, len(cases))
	seen := make(map[string]struct{}, len(cases))
	for _, test := range cases {
		if _, duplicate := seen[test.filename]; duplicate {
			return fmt.Errorf("duplicate MCP public example case %q", test.filename)
		}
		seen[test.filename] = struct{}{}
		tabled = append(tabled, test.filename)
	}
	if err := requireSameStringSet("documented and physical MCP public examples", linked, physical); err != nil {
		return err
	}
	return requireSameStringSet("documented and tabled MCP public examples", linked, tabled)
}

func requireSameStringSet(label string, left []string, right []string) error {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	slices.Sort(left)
	slices.Sort(right)
	if !slices.Equal(left, right) {
		return fmt.Errorf("%s differ: left=%v right=%v", label, left, right)
	}
	return nil
}

func TestMCPPublicExampleLinkInventoryRejectsAmbiguousDocumentation(t *testing.T) {
	validLink := "[example](../examples/claude-project-mcp-stdio.toml)"
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "missing marker", content: validLink, wantErr: true},
		{name: "duplicate marker", content: mcpPublicExampleInventoryMarker + " " + validLink + "\n" + mcpPublicExampleInventoryMarker, wantErr: true},
		{name: "no links", content: mcpPublicExampleInventoryMarker + " none\n\n", wantErr: true},
		{name: "duplicate link", content: mcpPublicExampleInventoryMarker + " " + validLink + " " + validLink + "\n\n", wantErr: true},
		{name: "nested link", content: mcpPublicExampleInventoryMarker + " [example](../examples/nested/example-mcp.toml)\n\n", wantErr: true},
		{name: "non TOML link", content: mcpPublicExampleInventoryMarker + " [example](../examples/example-mcp.md)\n\n", wantErr: true},
		{name: "CRLF inventory", content: mcpPublicExampleInventoryMarker + " " + validLink + "\r\n\r\nafter", wantErr: false},
		{name: "link outside inventory ignored", content: "[outside](../examples/outside-mcp.toml)\n\n" + mcpPublicExampleInventoryMarker + " " + validLink + "\n\n", wantErr: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseMCPPublicExampleLinks(test.content)
			if (err != nil) != test.wantErr {
				t.Fatalf("parse error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestMCPPublicExampleInventoryRejectsSetDrift(t *testing.T) {
	baseCases := []mcpPublicExampleCase{{filename: "a-mcp.toml"}, {filename: "b-mcp.toml"}}
	tests := []struct {
		name     string
		linked   []string
		physical []string
		cases    []mcpPublicExampleCase
		wantErr  bool
	}{
		{name: "order independent", linked: []string{"b-mcp.toml", "a-mcp.toml"}, physical: []string{"a-mcp.toml", "b-mcp.toml"}, cases: baseCases},
		{name: "linked file missing", linked: []string{"a-mcp.toml", "b-mcp.toml"}, physical: []string{"a-mcp.toml"}, cases: baseCases, wantErr: true},
		{name: "unlinked physical file", linked: []string{"a-mcp.toml"}, physical: []string{"a-mcp.toml", "b-mcp.toml"}, cases: []mcpPublicExampleCase{{filename: "a-mcp.toml"}}, wantErr: true},
		{name: "missing executable case", linked: []string{"a-mcp.toml", "b-mcp.toml"}, physical: []string{"a-mcp.toml", "b-mcp.toml"}, cases: []mcpPublicExampleCase{{filename: "a-mcp.toml"}}, wantErr: true},
		{name: "stale executable case", linked: []string{"a-mcp.toml"}, physical: []string{"a-mcp.toml"}, cases: baseCases, wantErr: true},
		{name: "duplicate executable case", linked: []string{"a-mcp.toml"}, physical: []string{"a-mcp.toml"}, cases: []mcpPublicExampleCase{{filename: "a-mcp.toml"}, {filename: "a-mcp.toml"}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMCPPublicExampleInventory(test.linked, test.physical, test.cases)
			if (err != nil) != test.wantErr {
				t.Fatalf("validation error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}
