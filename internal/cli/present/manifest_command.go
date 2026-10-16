package clipresent

import (
	"fmt"
	"io"
)

// ManifestCommand renders a manifest-scoped command, retaining the selector
// unless the CLI established the same implicit target for that command.
func ManifestCommand(command []string, manifestPath string, options HumanOptions, arguments ...string) (string, error) {
	argv := append([]string{"daem"}, command...)
	if manifestPath == "" || manifestPath != options.ImplicitManifestPath {
		argv = append(argv, "--manifest", manifestPath)
	}
	argv = append(argv, arguments...)

	return ShellCommand(argv...)
}

// PrintManifestCommand writes a concrete command only when its arguments can
// round-trip through the shared shell renderer.
func PrintManifestCommand(output io.Writer, prefix string, command []string, manifestPath string, options HumanOptions, arguments ...string) {
	text, err := ManifestCommand(command, manifestPath, options, arguments...)
	if err != nil {
		return
	}
	fmt.Fprintf(output, "%s%s\n", prefix, text)
}
