package codec

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestScannersRejectNonCanonicalFieldNames(t *testing.T) {
	for _, test := range []struct {
		name  string
		block string
		scan  func([]byte) error
	}{
		{"skill", "[[skill]]\nName=\"review\"\nsource={path=\"skills/review\"}\n", func(content []byte) error { _, err := ScanSkillBlocks(content); return err }},
		{"skill group", "[[skill_group]]\nNames=[\"review\"]\nsource={path=\"skills\"}\n", func(content []byte) error { _, err := ScanSkillGroupBlocks(content); return err }},
		{"hook", "[[hook]]\nName=\"guard\"\n", func(content []byte) error { _, err := ScanHookBlocks(content); return err }},
		{"extension", "[[extension]]\nID=\"tool\"\n", func(content []byte) error { _, err := ScanExtensionBlocks(content); return err }},
		{"instruction", "[instructions.guide]\nTargets=[\"codex\"]\nsource=\"notes.md\"\n", func(content []byte) error { _, err := ScanInstructionBlocks(content); return err }},
		{"mcp", "[[mcp_server]]\nName=\"docs\"\n", func(content []byte) error { _, err := ScanMCPServerBlocks(content); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte("version=1\n# preceding content\n\n" + test.block)
			err := test.scan(content)
			var positioned declaration.UnknownManifestKeyError
			if !errors.As(err, &positioned) {
				t.Fatalf("error=%v, want positioned scanner field failure", err)
			}
			line, column, ok := positioned.Position()
			if !ok || line != 5 || column != 1 {
				t.Fatalf("position=(%d,%d,%t), want (5,1,true)", line, column, ok)
			}
		})
	}
}
