package clipresent

import (
	"fmt"
	"io"
)

// ManifestCommand renders a manifest-scoped command, retaining the selector
// unless the CLI established the same implicit target for that command.
func ManifestCommand(command []string, manifestPath string, options HumanOptions, arguments ...string) (string, error) {
	return ShellCommand(ManifestCommandArguments(command, manifestPath, options, arguments...)...)
}

// ManifestCommandArguments preserves selector provenance before either shell or
// JSON projection. It does not resolve a workspace or reinterpret command data.
func ManifestCommandArguments(command []string, manifestPath string, options HumanOptions, arguments ...string) []string {
	argv := append([]string{"daem"}, command...)
	if manifestPath == "" || manifestPath != options.ImplicitManifestPath {
		request := manifestPath
		if options.ManifestRequest != nil {
			request = *options.ManifestRequest
		}
		if request != "" {
			argv = append(argv, "--manifest", request)
		}
	}
	argv = append(argv, arguments...)

	return argv
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
