package repair

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	"github.com/isty2e/daem/internal/target"
)

func classifyUnchanged(
	ctx context.Context,
	input artifact.ExactIdentity,
	view access.View,
	installName string,
	targets []target.Target,
) (bool, error) {
	sink := classificationDocumentSink{}
	// Capture from the hashed stream; an independent document read followed by
	// a tree hash would not bind the classified bytes to the supplied identity.
	if err := view.CopyVerified(ctx, input, &sink); err != nil {
		return false, fmt.Errorf("copy verified skill source for repair: %w", err)
	}
	content := sink.document
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return false, nil
	}
	frontmatter, err := skillcompat.ParseSkillFrontmatter(content)
	if err != nil {
		return false, nil
	}
	for _, selectedTarget := range targets {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		for _, diagnostic := range skillcompat.FrontmatterDiagnostics(input.SourceID(), installName, selectedTarget, frontmatter) {
			if diagnostic.Blocking() {
				return false, nil
			}
		}
	}
	return true, ctx.Err()
}

type classificationDocumentSink struct {
	document []byte
}

func (*classificationDocumentSink) BeginDirectory(string, fs.FileMode) error { return nil }
func (*classificationDocumentSink) EndDirectory(string, fs.FileMode) error   { return nil }

func (sink *classificationDocumentSink) OpenFile(relativePath string, _ fs.FileMode, size int64) (io.WriteCloser, error) {
	if relativePath != "SKILL.md" || size > skillcompat.MaximumSkillDocumentBytes {
		return classificationDocumentWriter{}, nil
	}
	sink.document = make([]byte, 0, int(size))
	return classificationDocumentWriter{document: &sink.document}, nil
}

type classificationDocumentWriter struct {
	document *[]byte
}

func (writer classificationDocumentWriter) Write(content []byte) (int, error) {
	if writer.document == nil {
		return len(content), nil
	}
	if err := skillcompat.CheckSkillDocumentSize(int64(len(*writer.document)) + int64(len(content))); err != nil {
		return 0, err
	}
	if len(content) > cap(*writer.document)-len(*writer.document) {
		return 0, fmt.Errorf("SKILL.md content exceeds observed size %d", cap(*writer.document))
	}
	*writer.document = append(*writer.document, content...)
	return len(content), nil
}

func (classificationDocumentWriter) Close() error { return nil }
