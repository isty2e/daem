package archguard

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	documentationLinkTargetRule    = "documentation-link-target"
	documentationLinkAnchorRule    = "documentation-link-anchor"
	documentationDeprecatedCLIRule = "documentation-deprecated-cli-grammar"
)

type documentationFinding struct {
	Rule   string
	Path   string
	Line   int
	Target string
	Detail string
}

type documentationReport struct {
	findings []documentationFinding
}

func (report documentationReport) hasFailures() bool {
	return len(report.findings) != 0
}

// analyzeDocumentation checks public repository links and retired CLI grammar.
// It supports the repository's relative inline links, reference definitions,
// ATX headings, inline code, and fenced code without claiming full CommonMark
// support.
func analyzeDocumentation(root string) (documentationReport, error) {
	documents, err := loadMarkdownDocuments(root)
	if err != nil {
		return documentationReport{}, err
	}

	byPath := make(map[string]markdownDocument, len(documents))
	for _, document := range documents {
		byPath[document.path] = document
	}

	var findings []documentationFinding
	for _, document := range documents {
		if isUserDocumentation(document.path) {
			for _, use := range document.deprecatedCLIUse {
				findings = append(findings, documentationFinding{
					Rule:   documentationDeprecatedCLIRule,
					Path:   document.path,
					Line:   use.line,
					Target: use.syntax,
					Detail: "user documentation contains superseded CUX command grammar",
				})
			}
		}
		for _, link := range document.links {
			resolved, fragment, local, resolveErr := resolveMarkdownDestination(document.path, link.destination)
			if resolveErr != nil {
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkTargetRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: resolveErr.Error(),
				})
				continue
			}
			if !local {
				continue
			}

			caseMismatch, caseErr := hasPathCaseMismatch(root, resolved)
			if caseErr != nil {
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkTargetRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: fmt.Sprintf("cannot verify local Markdown target case: %v", caseErr),
				})
				continue
			}
			if caseMismatch {
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkTargetRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: "local Markdown target does not match filesystem path case",
				})
				continue
			}

			targetPath := filepath.Join(root, filepath.FromSlash(resolved))
			info, statErr := os.Stat(targetPath)
			if statErr != nil {
				detail := "local Markdown target does not exist"
				if !os.IsNotExist(statErr) {
					detail = fmt.Sprintf("cannot inspect local Markdown target: %v", statErr)
				}
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkTargetRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: detail,
				})
				continue
			}

			anchorPath := resolved
			if info.IsDir() {
				anchorPath = filepath.ToSlash(filepath.Join(resolved, "README.md"))
			}
			if fragment == "" {
				continue
			}
			targetDocument, ok := byPath[anchorPath]
			if !ok {
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkAnchorRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: "fragment target is not a checked Markdown document",
				})
				continue
			}
			if _, ok := targetDocument.anchors[fragment]; !ok {
				findings = append(findings, documentationFinding{
					Rule:   documentationLinkAnchorRule,
					Path:   document.path,
					Line:   link.line,
					Target: link.destination,
					Detail: fmt.Sprintf("local Markdown anchor %q does not exist", fragment),
				})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Target < right.Target
	})
	return documentationReport{findings: findings}, nil
}

func formatDocumentationReport(report documentationReport) string {
	if len(report.findings) == 0 {
		return "documentation guard: no findings"
	}

	var output strings.Builder
	fmt.Fprintf(&output, "documentation guard: %d finding(s)\n", len(report.findings))
	for _, finding := range report.findings {
		fmt.Fprintf(&output, "- %s:%d [%s] %s", finding.Path, finding.Line, finding.Rule, finding.Detail)
		if finding.Target != "" {
			fmt.Fprintf(&output, " (target %q)", finding.Target)
		}
		output.WriteByte('\n')
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func resolveMarkdownDestination(sourcePath string, destination string) (string, string, bool, error) {
	parsed, err := url.Parse(destination)
	if err != nil {
		return "", "", true, fmt.Errorf("invalid local Markdown destination: %v", err)
	}
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(destination, "//") || strings.HasPrefix(parsed.Path, "/") {
		return "", "", false, nil
	}
	if parsed.RawQuery != "" {
		return "", "", true, fmt.Errorf("local Markdown destinations with queries are outside the guard contract")
	}

	decodedPath, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", "", true, fmt.Errorf("invalid escaped local Markdown path: %v", err)
	}
	fragment, err := url.PathUnescape(parsed.Fragment)
	if err != nil {
		return "", "", true, fmt.Errorf("invalid escaped local Markdown fragment: %v", err)
	}
	fragment = strings.ToLower(fragment)

	resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(decodedPath))))
	if decodedPath == "" {
		resolved = sourcePath
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", "", true, fmt.Errorf("local Markdown target escapes the repository root")
	}
	return resolved, fragment, true, nil
}

func hasPathCaseMismatch(root string, relativePath string) (bool, error) {
	current := root
	for component := range strings.SplitSeq(filepath.FromSlash(relativePath), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}

		exact := false
		caseFolded := false
		for _, entry := range entries {
			if entry.Name() == component {
				exact = true
				break
			}
			if strings.EqualFold(entry.Name(), component) {
				caseFolded = true
			}
		}
		if !exact {
			return caseFolded, nil
		}
		current = filepath.Join(current, component)
	}
	return false, nil
}

func isUserDocumentation(path string) bool {
	return path == "README.md" || path == "docs" || strings.HasPrefix(path, "docs/")
}
