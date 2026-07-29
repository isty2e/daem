package merge

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

func insertImportedExtensions(
	content []byte,
	ordered []desiredextension.Extension,
	additions []desiredextension.Extension,
) ([]byte, error) {
	if len(additions) == 0 {
		return append([]byte(nil), content...), nil
	}
	blocks, err := declarationcodec.ScanExtensionBlocks(content)
	if err != nil {
		return nil, err
	}
	existingByID := make(map[string]declarationcodec.ExtensionBlock, len(blocks))
	for _, block := range blocks {
		if _, duplicate := existingByID[block.Extension.ID]; duplicate {
			return nil, fmt.Errorf(
				"existing extension id %q appears more than once",
				block.Extension.ID,
			)
		}
		existingByID[block.Extension.ID] = block
	}
	additionsByID := make(map[string]desiredextension.Extension, len(additions))
	for _, addition := range additions {
		id := addition.ID().Name()
		if _, duplicate := additionsByID[id]; duplicate {
			return nil, fmt.Errorf("imported extension id %q appears more than once", id)
		}
		if _, collision := existingByID[id]; collision {
			return nil, fmt.Errorf("imported extension id %q already exists", id)
		}
		additionsByID[id] = addition
	}

	changes := make([]declaration.DocumentChange, 0, len(additions))
	pending := make([]desiredextension.Extension, 0)
	placed := make(map[string]struct{}, len(additions))
	lastExistingEnd := -1
	for _, extension := range ordered {
		id := extension.ID().Name()
		if addition, imported := additionsByID[id]; imported {
			pending = append(pending, addition)
			placed[id] = struct{}{}
			continue
		}
		block, exists := existingByID[id]
		if !exists {
			return nil, fmt.Errorf(
				"ordered extension %q is neither existing nor imported",
				id,
			)
		}
		if len(pending) != 0 {
			position := extensionLeadingCommentStart(content, block.Start)
			changes = append(changes, declaration.NewDocumentReplacement(
				declaration.DocumentRange{Start: position, End: position},
				renderExtensionInsertion(content, position, pending),
			))
			pending = pending[:0]
		}
		lastExistingEnd = block.End
	}
	if len(placed) != len(additionsByID) {
		return nil, fmt.Errorf("extension order proposal omits one or more imported relations")
	}
	if len(pending) != 0 {
		position := len(content)
		if lastExistingEnd >= 0 {
			position = lastExistingEnd
		}
		changes = append(changes, declaration.NewDocumentReplacement(
			declaration.DocumentRange{Start: position, End: position},
			renderExtensionInsertion(content, position, pending),
		))
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("extension order proposal produced no insertion")
	}
	return declaration.NewDocumentChangeSet(changes...).Apply(content)
}

func extensionLeadingCommentStart(content []byte, blockStart int) int {
	position := blockStart
	for position > 0 {
		lineEnd := position
		if content[lineEnd-1] == '\n' {
			lineEnd--
			if lineEnd > 0 && content[lineEnd-1] == '\r' {
				lineEnd--
			}
		}
		lineStart := bytes.LastIndexByte(content[:lineEnd], '\n') + 1
		if !strings.HasPrefix(strings.TrimSpace(string(content[lineStart:lineEnd])), "#") {
			break
		}
		position = lineStart
	}
	return position
}

func renderExtensionInsertion(
	content []byte,
	position int,
	extensions []desiredextension.Extension,
) []byte {
	lineEnding := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	rendered := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		block := strings.TrimSpace(
			declarationcodec.RenderExtensionBlock(extensionDeclaration(extension)),
		)
		rendered = append(rendered, strings.ReplaceAll(block, "\n", lineEnding))
	}
	body := strings.Join(rendered, lineEnding+lineEnding)

	var output strings.Builder
	before := content[:position]
	after := content[position:]
	if len(before) != 0 {
		if !bytes.HasSuffix(before, []byte(lineEnding)) {
			output.WriteString(lineEnding)
		}
		if !bytes.HasSuffix(before, []byte(lineEnding+lineEnding)) {
			output.WriteString(lineEnding)
		}
	}
	output.WriteString(body)
	output.WriteString(lineEnding)
	if len(after) != 0 && !bytes.HasPrefix(after, []byte(lineEnding)) {
		output.WriteString(lineEnding)
	}
	return []byte(output.String())
}

func extensionDeclaration(extension desiredextension.Extension) declaration.Extension {
	source := declaration.ExtensionSource{}
	switch extension.Source().Kind() {
	case desiredextension.SourceKindMarketplace:
		source.Marketplace = extension.Source().Ref()
	case desiredextension.SourceKindHostSource:
		source.HostSource = extension.Source().Ref()
	}
	return declaration.Extension{
		ID:      extension.ID().Name(),
		Carrier: string(extension.Carrier()),
		Targets: []string{string(extension.Target())},
		Scope:   string(extension.Scope()),
		Source:  source,
	}
}
