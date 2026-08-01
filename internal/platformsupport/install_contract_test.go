package platformsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const installMacOSRuntimeFunction = `daem_admitted_macos_product_version() {
  /usr/bin/awk -F. '
    BEGIN { valid = 1 }
    NR > 1 { valid = 0; next }
    NF < 2 || NF > 3 { valid = 0; next }
    {
      for (field = 1; field <= NF; field++) {
        if ($field !~ /^(0|[1-9][0-9]*)$/) { valid = 0; next }
        if (length($field) > 10 || (length($field) == 10 && ($field + 0) > 4294967295)) { valid = 0; next }
      }
      if (($1 + 0) < 26) { valid = 0; next }
      version = $0
    }
    END {
      if (NR != 1 || valid != 1) exit 1
      print version
    }
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
		`/usr/bin/sw_vers --productVersion > "$DAEM_MACOS_VERSION_FILE"`,
		`DAEM_MACOS_VERSION="$(daem_admitted_macos_product_version < "$DAEM_MACOS_VERSION_FILE")"`,
		`requires macOS 26.0 or newer`,
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("docs/install.md is missing runtime contract %q", required)
		}
	}
	if strings.Contains(document, `DAEM_MACOS_VERSION="$(/usr/bin/sw_vers --productVersion)"`) {
		t.Fatal("docs/install.md captures raw sw_vers output through command substitution")
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
		command := exec.Command("/bin/sh", "-c", installMacOSRuntimeFunction+"\n"+`printf '%s' "$1" | daem_admitted_macos_product_version >/dev/null`, "daem-install-test", test.version)
		err := command.Run()
		if (err == nil) != test.supported {
			t.Fatalf("installer macOS %q error = %v, want supported=%t", test.version, err, test.supported)
		}
	}
}

func TestInstallRecipePreservesRawMacOSProductVersionOutput(t *testing.T) {
	fixtureRoot := t.TempDir()
	fixturePath := filepath.Join(fixtureRoot, "sw-vers-fixture")
	if err := os.WriteFile(fixturePath, []byte("#!/bin/sh\nprintf '%s' \"$DAEM_TEST_SW_VERS_OUTPUT\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		output    string
		canonical string
	}{
		{name: "no trailing newline", output: "26.0", canonical: "26.0"},
		{name: "one trailing newline", output: "26.0\n", canonical: "26.0"},
		{name: "two trailing newlines", output: "26.0\n\n"},
		{name: "multiple lines", output: "26.0\n27.0\n"},
		{name: "carriage return", output: "26.0\r\n"},
		{name: "below floor", output: "25.9.9\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capturePath := filepath.Join(t.TempDir(), "macos-product-version")
			script := installMacOSRuntimeFunction + `
if ! "$1" > "$2"; then
  exit 1
fi
DAEM_MACOS_VERSION="$(daem_admitted_macos_product_version < "$2")" || exit 1
test "$DAEM_MACOS_VERSION" = "$3"
`
			command := exec.Command("/bin/sh", "-c", script, "daem-install-test", fixturePath, capturePath, test.canonical)
			command.Env = append(os.Environ(), "DAEM_TEST_SW_VERS_OUTPUT="+test.output)
			err := command.Run()
			if (err == nil) != (test.canonical != "") {
				t.Fatalf("raw output %q error = %v, want canonical %q", test.output, err, test.canonical)
			}
		})
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
