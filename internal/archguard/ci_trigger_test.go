package archguard

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIEventAdmissionDoesNotDuplicatePullRequestHeads(t *testing.T) {
	path := filepath.Join(findRepoRoot(t), ".github", "workflows", "ci.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode CI workflow: %v", err)
	}
	root := requireYAMLMappingValue(t, requireYAMLDocumentRoot(t, document), "on")
	pullRequest := requireYAMLMappingValue(t, root, "pull_request")
	if !emptyYAMLConfiguration(pullRequest) {
		t.Fatalf(
			"CI pull_request trigger kind/tag/content = %d/%q/%d, want no filters",
			pullRequest.Kind,
			pullRequest.Tag,
			len(pullRequest.Content),
		)
	}

	push := requireYAMLMappingValue(t, root, "push")
	if push.Kind != yaml.MappingNode || len(push.Content) != 2 || push.Content[0].Value != "branches" {
		t.Fatalf("CI push trigger kind/content = %d/%d, want only branches", push.Kind, len(push.Content))
	}
	branches := requireYAMLMappingValue(t, push, "branches")
	if branches.Kind != yaml.SequenceNode || len(branches.Content) != 1 || branches.Content[0].Value != "main" {
		t.Fatalf("CI push branches kind/content = %d/%d, want exactly [main]", branches.Kind, len(branches.Content))
	}
}

func emptyYAMLConfiguration(node *yaml.Node) bool {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return true
	}
	return node.Kind == yaml.MappingNode && len(node.Content) == 0
}

func requireYAMLDocumentRoot(t *testing.T, document yaml.Node) *yaml.Node {
	t.Helper()
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		t.Fatalf("YAML document kind/content = %d/%d, want one document root", document.Kind, len(document.Content))
	}
	return document.Content[0]
}

func requireYAMLMappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping.Kind != yaml.MappingNode {
		t.Fatalf("YAML node for %q has kind %d, want mapping", key, mapping.Kind)
	}
	var found *yaml.Node
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			if found != nil {
				t.Fatalf("YAML mapping contains duplicate %q keys", key)
			}
			found = mapping.Content[index+1]
		}
	}
	if found == nil {
		t.Fatalf("YAML mapping does not contain %q", key)
	}
	return found
}
