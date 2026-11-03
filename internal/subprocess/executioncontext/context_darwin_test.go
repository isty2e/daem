package executioncontext

import (
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
