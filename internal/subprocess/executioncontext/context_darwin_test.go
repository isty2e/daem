package executioncontext

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestDarwinPlatformIdentityDocumentRequiresOneNonemptyUUID(t *testing.T) {
	pair := "<key>IOPlatformUUID</key><string>12345678-1234-1234-1234-123456789ABC</string>"
	valid := "<plist><array><dict>" + pair + "</dict></array></plist>"
	value, err := darwinExecutionHostID(valid)
	if err != nil || value != "12345678123412341234123456789abc" {
		t.Fatal("system identity document did not preserve its canonical UUID", err)
	}
	for _, input := range []string{
		"<plist><dict></dict></plist>",
		"<plist><dict>" + pair + pair + "</dict></plist>",
		"<plist><dict><key>IOPlatformUUID</key><string></string>" + pair + "</dict></plist>",
		strings.ReplaceAll(valid, "12345678-1234-1234-1234-123456789ABC", "private-malformed-identity"),
		"<plist><dict><key>IOPlatformUUID</key><data>123</data></dict></plist>",
		"<plist><dict><key>IOPlatformUUID</key></dict></plist>",
		"<plist><dict><key>IOPlatformUUID</key></dict><string>12345678-1234-1234-1234-123456789ABC</string></plist>",
	} {
		_, err := darwinExecutionHostID(input)
		if err == nil || strings.Contains(err.Error(), "private-malformed-identity") {
			t.Fatal("missing, ambiguous or malformed identity was accepted or exposed")
		}
	}
}

func TestDarwinExecutionContextOutputBoundsCommandCapture(t *testing.T) {
	for _, test := range []struct {
		name     string
		size     int
		overflow bool
	}{
		{"at_limit", 1 << 20, false},
		{"oversized", 2 << 20, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output executionContextOutput
			command := exec.CommandContext(t.Context(), os.Args[0],
				"-test.run=^TestDarwinExecutionContextOutputHelper$", "--",
				"execution-context-output", strconv.Itoa(test.size))
			command.Stdout, command.Stderr = &output, io.Discard
			err := command.Run()
			if test.overflow {
				if err == nil || len(output.String()) > 1<<20 {
					t.Fatalf("oversized command output was accepted or retained: bytes=%d error=%v", len(output.String()), err)
				}
				return
			}
			if err != nil || output.String() != strings.Repeat("x", test.size) {
				t.Fatalf("bounded helper output was not captured intact: bytes=%d error=%v", len(output.String()), err)
			}
		})
	}
}

func TestDarwinExecutionContextOutputHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "execution-context-output" {
		return
	}
	size, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(2)
	}
	if _, err := os.Stdout.Write(bytes.Repeat([]byte("x"), size)); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
