package skillcompat

import (
	"regexp"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
)

var strictSkillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func frontmatterDiagnostics(
	sourceID artifact.SourceID,
	installName string,
	profile profile,
	frontmatter SkillFrontmatter,
) []Diagnostic {
	rules := profile.Frontmatter
	diagnostics := make([]Diagnostic, 0)
	name := strings.TrimSpace(frontmatter.Name)
	if rules.RequireName && name == "" {
		diagnostics = append(diagnostics, errorDiagnostic(
			AxisFrontmatter,
			"missing-name",
			"skill source %q target %q: SKILL.md frontmatter name is required",
			sourceID,
			profile.Target,
		))
	}
	if rules.StrictName && name != "" && !strictSkillNamePattern.MatchString(name) {
		diagnostics = append(diagnostics, errorDiagnostic(
			AxisIdentity,
			"invalid-name",
			"skill source %q target %q: SKILL.md frontmatter name %q must match %s",
			sourceID,
			profile.Target,
			name,
			strictSkillNamePattern.String(),
		))
	}
	if rules.WarnInvalidName && name != "" && !strictSkillNamePattern.MatchString(name) {
		diagnostics = append(diagnostics, warningDiagnostic(
			AxisIdentity,
			"non-portable-name",
			"skill source %q target %q: SKILL.md frontmatter name %q does not match portable pattern %s",
			sourceID,
			profile.Target,
			name,
			strictSkillNamePattern.String(),
		))
	}
	if rules.MaxNameLength > 0 && len([]rune(name)) > rules.MaxNameLength {
		if rules.WarnNameLength {
			diagnostics = append(diagnostics, warningDiagnostic(
				AxisIdentity,
				"name-too-long",
				"skill source %q target %q: SKILL.md frontmatter name exceeds %d characters",
				sourceID,
				profile.Target,
				rules.MaxNameLength,
			))
		} else {
			diagnostics = append(diagnostics, errorDiagnostic(
				AxisIdentity,
				"name-too-long",
				"skill source %q target %q: SKILL.md frontmatter name exceeds %d characters",
				sourceID,
				profile.Target,
				rules.MaxNameLength,
			))
		}
	}
	if rules.RequireNameMatchesFolder && name != "" && name != installName {
		diagnostics = append(diagnostics, errorDiagnostic(
			AxisIdentity,
			"name-install-name-mismatch",
			"skill source %q target %q: SKILL.md frontmatter name %q must match skill name %q",
			sourceID,
			profile.Target,
			name,
			installName,
		))
	}

	description := strings.TrimSpace(frontmatter.Description)
	if rules.RequireDescription && description == "" {
		diagnostics = append(diagnostics, errorDiagnostic(
			AxisFrontmatter,
			"missing-description",
			"skill source %q target %q: SKILL.md frontmatter description is required",
			sourceID,
			profile.Target,
		))
	}
	if rules.RecommendDescription && description == "" {
		diagnostics = append(diagnostics, warningDiagnostic(
			AxisSelection,
			"recommended-description-missing",
			"skill source %q target %q: SKILL.md frontmatter description is recommended for automatic skill selection",
			sourceID,
			profile.Target,
		))
	}
	if rules.MaxDescriptionLength > 0 && len([]rune(description)) > rules.MaxDescriptionLength {
		if rules.WarnDescriptionLength {
			diagnostics = append(diagnostics, warningDiagnostic(
				AxisSelection,
				"description-too-long",
				"skill source %q target %q: SKILL.md frontmatter description exceeds %d characters",
				sourceID,
				profile.Target,
				rules.MaxDescriptionLength,
			))
		} else {
			diagnostics = append(diagnostics, errorDiagnostic(
				AxisSelection,
				"description-too-long",
				"skill source %q target %q: SKILL.md frontmatter description exceeds %d characters",
				sourceID,
				profile.Target,
				rules.MaxDescriptionLength,
			))
		}
	}

	diagnostics = append(diagnostics, unsupportedFrontmatterFieldDiagnostics(sourceID, profile, frontmatter)...)
	return diagnostics
}

func unsupportedFrontmatterFieldDiagnostics(
	sourceID artifact.SourceID,
	profile profile,
	frontmatter SkillFrontmatter,
) []Diagnostic {
	recognizedFields := make(map[string]struct{}, len(profile.ControlFields.RecognizedFrontmatterFields))
	for _, field := range profile.ControlFields.RecognizedFrontmatterFields {
		recognizedFields[field] = struct{}{}
	}
	if len(recognizedFields) == 0 || len(frontmatter.Fields) == 0 {
		return nil
	}

	unknownFields := make([]string, 0)
	for field := range frontmatter.Fields {
		if _, ok := recognizedFields[field]; !ok {
			unknownFields = append(unknownFields, field)
		}
	}
	sort.Strings(unknownFields)

	diagnostics := make([]Diagnostic, 0, len(unknownFields))
	for _, field := range unknownFields {
		diagnostics = append(diagnostics, warningDiagnostic(
			AxisControlField,
			"unrecognized-frontmatter-field",
			"skill source %q target %q: SKILL.md frontmatter field %q is not recognized by this target and may be ignored",
			sourceID,
			profile.Target,
			field,
		))
	}
	return diagnostics
}
