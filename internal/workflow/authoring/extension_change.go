package authoring

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

func BuildAddExtensionChange(document ManifestDocument, request AddExtensionRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	header, err := declaration.DecodeManifestHeader(document.Content)
	if err != nil {
		return Change{}, err
	}
	id, err := CleanExtensionID(request.ID)
	if err != nil {
		return Change{}, err
	}
	request.ID = id
	extension, err := ExtensionFromAddRequest(request, header, document.Paths.ManifestOrigin)
	if err != nil {
		return Change{}, err
	}

	content, changeKind, err := ApplyAddExtensionToManifest(document.Content, extension, header)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath:  document.Path,
		Original:      document.Content,
		Content:       content,
		ResourceID:    id,
		ChangeKind:    changeKind,
		ManifestBlock: strings.TrimRight(declarationcodec.RenderExtensionBlock(extension), "\n"),
	}, nil
}

func BuildRemoveExtensionChange(document ManifestDocument, request RemoveExtensionRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	id, err := CleanExtensionID(request.ID)
	if err != nil {
		return Change{}, err
	}
	request.ID = id
	content, changeKind, err := ApplyRemoveExtensionToManifest(document.Content, request)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath: document.Path,
		Original:     document.Content,
		Content:      content,
		ResourceID:   id,
		ChangeKind:   changeKind,
	}, nil
}

func ExtensionFromAddRequest(request AddExtensionRequest, header declaration.ManifestHeader, origin daempaths.ManifestOrigin) (declaration.Extension, error) {
	id, err := CleanExtensionID(request.ID)
	if err != nil {
		return declaration.Extension{}, err
	}
	carrier, err := extensionAuthoringCarrierFromAddRequest(request, header, origin)
	if err != nil {
		return declaration.Extension{}, err
	}
	rawScope := extensionAuthoringScopeInput(request.Scope, origin)
	scope, err := extensionAuthoringScope(carrier, rawScope)
	if err != nil {
		return declaration.Extension{}, err
	}
	source, err := extensionAuthoringSource(carrier, request.Source)
	if err != nil {
		return declaration.Extension{}, err
	}
	admittedTarget, ok := carrier.AdmittedTarget()
	if !ok {
		return declaration.Extension{}, fmt.Errorf("unsupported extension carrier %q", carrier)
	}

	return declaration.Extension{
		ID:      id,
		Carrier: string(carrier),
		Targets: []string{string(admittedTarget)},
		Scope:   scope,
		Source:  source,
	}, nil
}

func ApplyAddExtensionToManifest(original []byte, extension declaration.Extension, header declaration.ManifestHeader) ([]byte, string, error) {
	incomingSubject, err := extensionAuthoringSubject(extension, header, "incoming extension")
	if err != nil {
		return nil, "", err
	}

	blocks, err := declarationcodec.ScanExtensionBlocks(original)
	if err != nil {
		return nil, "", err
	}
	for _, block := range blocks {
		existing := block.Extension
		existingSubject, err := extensionAuthoringSubject(existing, header, fmt.Sprintf("extension %q", existing.ID))
		if err != nil {
			return nil, "", err
		}
		if existing.ID == extension.ID {
			if declarationcodec.SameExtensionRelation(existing, extension) {
				return nil, "", fmt.Errorf("extension %q already exists", extension.ID)
			}
			return nil, "", fmt.Errorf("duplicate extension id %q", extension.ID)
		}
		if existingSubject == incomingSubject {
			return nil, "", fmt.Errorf("duplicate extension relation subject %q for source %q", incomingSubject, extension.Source.Ref())
		}
	}

	return declaration.AppendDocumentBlock(original, declarationcodec.RenderExtensionBlock(extension)), "append extension resource", nil
}

func ApplyRemoveExtensionToManifest(original []byte, request RemoveExtensionRequest) ([]byte, string, error) {
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return nil, "", err
	}
	id, err := CleanExtensionID(request.ID)
	if err != nil {
		return nil, "", err
	}
	request.ID = id
	request, err = request.normalizedSelector()
	if err != nil {
		return nil, "", err
	}

	candidates, err := removeExtensionCandidates(original, header)
	if err != nil {
		return nil, "", err
	}
	matches := filterRemoveExtensionCandidates(candidates, request)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("extension resource %q not found", request.ID)
	}
	if len(matches) > 1 {
		return nil, "", fmt.Errorf("extension resource %q is ambiguous; narrow with --target/--scope", request.ID)
	}

	content := declaration.RemoveDocumentRange(original, declaration.DocumentRange{Start: matches[0].start, End: matches[0].end})
	return content, "remove extension resource", nil
}

func extensionAuthoringSubject(extension declaration.Extension, header declaration.ManifestHeader, context string) (string, error) {
	carrier, ok := extensionAuthoringCarrierFor(extension.Carrier)
	if !ok {
		return "", fmt.Errorf("%s.carrier: unsupported extension carrier %q", context, extension.Carrier)
	}
	admittedTarget, _ := carrier.AdmittedTarget()
	label := extensionAuthoringLabel(carrier)
	effectiveTargets := header.EffectiveTargets(extension.Targets)
	if len(effectiveTargets) != 1 {
		return "", fmt.Errorf("%s.targets: %s supports exactly one target", context, label)
	}
	selectedTarget, err := target.ParseTarget(effectiveTargets[0])
	if err != nil || selectedTarget != admittedTarget {
		return "", fmt.Errorf("%s.targets: %s supports only target %q", context, label, admittedTarget)
	}
	scopeValue := header.EffectiveScope(extension.Scope)
	selectedScope, err := target.ParseScope(scopeValue)
	if err != nil || !carrier.AdmitsTargetScope(selectedTarget, selectedScope) {
		return "", fmt.Errorf("%s.scope: %s supports only %s scope", context, label, strings.Join(extensionAuthoringScopeNames(carrier), " or "))
	}
	sourceRef, err := extensionCanonicalSourceRef(extension, carrier, context)
	if err != nil {
		return "", err
	}
	canonical, err := desiredextension.New(desiredextension.Spec{
		Name:    extension.ID,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   selectedScope,
		Source:  sourceRef,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", context, err)
	}
	key := canonical.CarrierKey()
	return strings.Join([]string{
		string(key.Target()),
		string(key.Scope()),
		string(key.Source().Kind()),
		key.Source().Ref(),
	}, "."), nil
}

func removeExtensionCandidates(content []byte, header declaration.ManifestHeader) ([]removeExtensionCandidate, error) {
	blocks, err := declarationcodec.ScanExtensionBlocks(content)
	if err != nil {
		return nil, err
	}
	candidates := make([]removeExtensionCandidate, 0, len(blocks))
	for _, block := range blocks {
		extension := block.Extension
		if _, err := extensionAuthoringSubject(extension, header, fmt.Sprintf("extension %q", extension.ID)); err != nil {
			return nil, err
		}
		candidates = append(candidates, removeExtensionCandidate{
			id:      extension.ID,
			scope:   header.EffectiveScope(extension.Scope),
			targets: header.EffectiveTargets(extension.Targets),
			start:   block.Start,
			end:     block.End,
		})
	}
	return candidates, nil
}

func filterRemoveExtensionCandidates(candidates []removeExtensionCandidate, request RemoveExtensionRequest) []removeExtensionCandidate {
	matches := make([]removeExtensionCandidate, 0)
	for _, candidate := range candidates {
		if candidate.id != request.ID {
			continue
		}
		if request.Scope != "" && candidate.scope != request.Scope {
			continue
		}
		if len(request.Targets) != 0 && !declaration.Targets(candidate.targets).Intersects(declaration.Targets(request.Targets)) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

type removeExtensionCandidate struct {
	id      string
	scope   string
	targets []string
	start   int
	end     int
}

func validateAuthoringStableToken(value string, context string) error {
	if value == "" {
		return fmt.Errorf("%s is required", context)
	}
	if !isASCIIAlphaNumeric(value[0]) {
		return fmt.Errorf("%s must start with an ASCII letter or digit", context)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlphaNumeric(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", context)
	}
	return nil
}
