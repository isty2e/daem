package profile

import "testing"

func TestPiMCPAdapterSourceIntentDoesNotDependOnInstallerOrVersionAdmission(t *testing.T) {
	for _, test := range []struct {
		source string
		known  bool
	}{
		{"npm:pi-mcp-adapter", true},
		{"npm:pi-mcp-adapter@latest", true},
		{"npm:pi-mcp-adapter-proxy@2.15.0", false},
		{"git:github.com/nicobailon/pi-mcp-adapter@main", true},
		{"git:github:nicobailon/pi-mcp-adapter@main", true},
		{"https://github.com/nicobailon/pi-mcp-adapter.git", true},
		{"git://github.com/nicobailon/pi-mcp-adapter.git", true},
		{"git:git@github.com:nicobailon/pi-mcp-adapter.git", true},
		{"git:git+https://github.com/nicobailon/pi-mcp-adapter.git", true},
		{"git:github.com/someone/pi-mcp-adapter", false},
		{"git:github.com/nicobailon/pi-mcp-adapter-other", false},
		{"/checkout/pi-mcp-adapter", false},
		{"git@github.com:nicobailon/pi-mcp-adapter.git", false},
		{"github:nicobailon/pi-mcp-adapter", false},
		{"git+https://github.com/nicobailon/pi-mcp-adapter.git", false},
	} {
		if got, err := PiMCPAdapterPackageSource(test.source); err != nil || got != test.known {
			t.Errorf("source %q intent=%t, %v; want %t", test.source, got, err, test.known)
		}
	}
}

func TestPiMCPAdapterSourceRefusesUnobservableGitIdentity(t *testing.T) {
	for _, source := range []string{"git:nicobailon/pi-mcp-adapter", "git:gitlab:owner/repo", "git:", "https://", "http://localhost/adapter"} {
		if known, err := PiMCPAdapterPackageSource(source); err == nil || known {
			t.Errorf("unmodelled source %q became observable absence: %t, %v", source, known, err)
		}
	}
}

func TestPiMCPNativeLocalPackageCarrierEnvelope(t *testing.T) {
	for _, source := range []string{
		"./adapter", "builtin:fixture/index.ts", "http:fixture/index.ts", "https:fixture/index.ts", "ssh:fixture/index.ts",
		"github:nicobailon/pi-mcp-adapter", "git+https://github.com/nicobailon/pi-mcp-adapter.git",
		"HTTPS://github.com/nicobailon/pi-mcp-adapter.git", " npm:pi-mcp-adapter ",
	} {
		if !PiMCPPackageSourceIsLocal(source) {
			t.Errorf("host-local source %q became a nonlocal carrier", source)
		}
	}
	for _, source := range []string{
		"npm:pi-mcp-adapter", "git:github:nicobailon/pi-mcp-adapter", " git:github.com/owner/repo ",
		"https://github.com/owner/repo", "http://localhost/owner/repo", "ssh://git@github.com/owner/repo",
	} {
		if PiMCPPackageSourceIsLocal(source) {
			t.Errorf("nonlocal carrier %q became a local resource path", source)
		}
	}
}
