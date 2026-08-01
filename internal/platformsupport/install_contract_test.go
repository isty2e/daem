package platformsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const installMacOSRuntimeFunction = `daem_macos_supported() {
  printf '%s\n' "$1" | /usr/bin/awk -F. '
    NR > 1 { exit 1 }
    NF < 2 || NF > 3 { exit 1 }
    {
      for (field = 1; field <= NF; field++) {
        if ($field !~ /^(0|[1-9][0-9]*)$/) exit 1
        if (length($field) > 10 || (length($field) == 10 && ($field + 0) > 4294967295)) exit 1
      }
      if (($1 + 0) < 26) exit 1
    }
    END { if (NR != 1) exit 1 }
  '
}`

func TestInstallRecipeUsesExecutableMacOSRuntimeFloor(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "docs", "install.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(content)
	for _, required := range []string{
		installMacOSRuntimeFunction,
		`/usr/bin/sw_vers --productVersion`,
		`requires macOS 26.0 or newer`,
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("docs/install.md is missing runtime contract %q", required)
		}
	}

	tests := []struct {
		version   string
		supported bool
	}{
		{version: ""},
		{version: "25.9.9"},
		{version: "26"},
		{version: "026.0"},
		{version: "26.00"},
		{version: "26.x"},
		{version: "26.0 "},
		{version: "26.0\n27.0"},
		{version: "26.0.0.0"},
		{version: "4294967296.0"},
		{version: "26.4294967296"},
		{version: "26.0", supported: true},
		{version: "26.5.1", supported: true},
		{version: "27.0", supported: true},
		{version: "4294967295.0", supported: true},
	}
	for _, test := range tests {
		command := exec.Command("/bin/sh", "-c", installMacOSRuntimeFunction+"\n"+`daem_macos_supported "$1"`, "daem-install-test", test.version)
		err := command.Run()
		if (err == nil) != test.supported {
			t.Fatalf("installer macOS %q error = %v, want supported=%t", test.version, err, test.supported)
		}
	}
}

func TestMacOSRuntimeFloorMatchesPublicContracts(t *testing.T) {
	admission, err := Lookup("darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	minimum, required := admission.RuntimeRequirement()
	if !required || minimum.String() != "26.0" {
		t.Fatalf("Darwin runtime requirement = %s,%t", minimum, required)
	}
	for _, path := range []string{"README.md", "docs/install.md", "docs/platforms.md"} {
		content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "macOS 26") {
			t.Fatalf("%s does not disclose macOS 26 floor", path)
		}
	}
}
