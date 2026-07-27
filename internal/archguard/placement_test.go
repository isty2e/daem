package archguard

import (
	"reflect"
	"strings"
	"testing"
)

var commandPackageAdmissions = map[string]string{
	"cmd/daem": "primary CLI executable composition root",
}

func TestBuildSelectedRepositoryPackagesHaveExactArchitectureOwnership(t *testing.T) {
	const modulePath = "github.com/isty2e/daem/"

	records := loadRepoPackageRecords(t)
	currentPackages := make(map[string]struct{})
	seenCommands := make(map[string]bool, len(commandPackageAdmissions))
	for _, record := range records {
		if len(productionFiles(record)) == 0 {
			continue
		}
		if !strings.HasPrefix(record.ImportPath, modulePath) {
			t.Fatalf("repository package %q is outside module %q", record.ImportPath, modulePath)
		}
		packagePath := strings.TrimPrefix(record.ImportPath, modulePath)
		switch {
		case strings.HasPrefix(packagePath, "internal/"):
			currentPackages[packagePath] = struct{}{}
			candidates := packagePlacementCandidates(packagePlacementRows, packagePath)
			if len(candidates) != 1 {
				t.Errorf("%s has %d Pi placements, want exactly one", packagePath, len(candidates))
				continue
			}
			if err := candidates[0].validate(); err != nil {
				t.Errorf("%s placement is invalid: %v", packagePath, err)
			}
		case strings.HasPrefix(packagePath, "test/"):
			if _, admitted := testToolPackageAdmissions[packagePath]; !admitted {
				t.Errorf("%s has no exact test/tool architecture admission", packagePath)
			}
		case strings.HasPrefix(packagePath, "cmd/"):
			if _, admitted := commandPackageAdmissions[packagePath]; !admitted {
				t.Errorf("%s has no exact command-root architecture admission", packagePath)
			} else {
				seenCommands[packagePath] = true
			}
		default:
			t.Errorf("%s is outside the admitted command, internal, and test architecture roots", packagePath)
		}
	}

	if findings := analyzePackagePlacements(records); len(findings) != 0 {
		t.Fatalf("current Pi placement findings:\n%s", FormatReport(findings))
	}
	for _, row := range packagePlacementRows {
		for _, packagePath := range row.packages {
			if _, present := currentPackages[packagePath]; !present {
				t.Errorf("placement row %q contains stale package %q", row.id, packagePath)
			}
		}
	}
	for packagePath := range commandPackageAdmissions {
		if !seenCommands[packagePath] {
			t.Errorf("command-root architecture admission %q is stale", packagePath)
		}
	}
}

func TestPackagePlacementKeepsAffinityRoleAndSpecializationIndependent(t *testing.T) {
	tests := []struct {
		packagePath        string
		wantAffinity       semanticAffinity
		wantRole           mechanismRole
		wantSpecialization packageSpecialization
	}{
		{
			packagePath:  "internal/desired/skill",
			wantAffinity: affinityDesired,
			wantRole:     roleSemanticKernel,
			wantSpecialization: packageSpecialization{
				kind: specializationFamily, value: "Skill",
			},
		},
		{
			packagePath:  "internal/declaration/codec",
			wantAffinity: affinityDesired,
			wantRole:     roleCodec,
			wantSpecialization: packageSpecialization{
				kind: specializationFormat, value: "TOML",
			},
		},
		{
			packagePath:  "internal/assurance/observe/codexplugin",
			wantAffinity: affinityAssurance,
			wantRole:     roleObservationAdapter,
			wantSpecialization: packageSpecialization{
				kind: specializationHost, value: "Codex",
			},
		},
		{
			packagePath:  "internal/platformsupport",
			wantAffinity: affinityNone,
			wantRole:     roleSemanticKernel,
			wantSpecialization: packageSpecialization{
				kind: specializationPlatform, value: "GOOS/GOARCH",
			},
		},
		{
			packagePath:        "internal/findings",
			wantAffinity:       affinityNone,
			wantRole:           roleStableValue,
			wantSpecialization: packageSpecialization{kind: specializationNone},
		},
	}

	for _, test := range tests {
		t.Run(test.packagePath, func(t *testing.T) {
			placement, ok := packagePlacementFor(test.packagePath)
			if !ok {
				t.Fatalf("%s has no unique valid placement", test.packagePath)
			}
			if placement.affinity != test.wantAffinity ||
				placement.role != test.wantRole ||
				placement.specialization != test.wantSpecialization {
				t.Fatalf(
					"%s placement = %+v, want affinity=%s role=%s specialization=%+v",
					test.packagePath,
					placement,
					test.wantAffinity,
					test.wantRole,
					test.wantSpecialization,
				)
			}
		})
	}
}

func TestPackagePlacementRejectsUnknownAndImplicitDescendantPackages(t *testing.T) {
	for _, packagePath := range []string{
		"internal/future",
		"internal/desired/future",
		"internal/assurance/observe/codexplugin/private",
		"internal/output/ownership/future",
		"internal/supply/source/backend/future",
	} {
		t.Run(packagePath, func(t *testing.T) {
			if _, ok := packagePlacementFor(packagePath); ok {
				t.Fatalf("%s inherited a Pi placement", packagePath)
			}
			findings := analyzePackagePlacements([]PackageRecord{{
				ImportPath: "example.com/project/" + packagePath,
			}})
			if countViolationRule(findings, rulePackagePlacementOwnership) != 1 {
				t.Fatalf("findings:\n%s", FormatReport(findings))
			}
		})
	}
}

func TestPackagePlacementCoversBuildSelectedCgoOnlyPackages(t *testing.T) {
	known := PackageRecord{
		ImportPath: "example.com/project/internal/platformsupport",
		CgoFiles:   []string{"platform_cgo.go"},
	}
	if findings := analyzePackagePlacements([]PackageRecord{known}); len(findings) != 0 {
		t.Fatalf("known Cgo-only package findings:\n%s", FormatReport(findings))
	}

	unknown := PackageRecord{
		ImportPath: "example.com/project/internal/platformsupport/future",
		CgoFiles:   []string{"platform_cgo.go"},
	}
	findings := analyzePackagePlacements([]PackageRecord{unknown})
	if countViolationRule(findings, rulePackagePlacementOwnership) != 1 {
		t.Fatalf("unknown Cgo-only package findings:\n%s", FormatReport(findings))
	}
}

func TestPackagePlacementMetadataFailsClosed(t *testing.T) {
	valid := packagePlacementRow{
		id:        "valid",
		placement: plainPlacement(affinityDesired, roleSemanticKernel),
		packages:  []string{"internal/desired"},
	}
	tests := map[string][]packagePlacementRow{
		"missing row id": {
			{placement: valid.placement, packages: valid.packages},
		},
		"duplicate row id": {
			valid,
			{id: valid.id, placement: valid.placement, packages: []string{"internal/desired/entity"}},
		},
		"empty package set": {
			{id: "empty", placement: valid.placement},
		},
		"unknown affinity": {
			{id: "affinity", placement: plainPlacement(affinityUnknown, roleSemanticKernel), packages: valid.packages},
		},
		"unknown role": {
			{id: "role", placement: plainPlacement(affinityDesired, roleUnknown), packages: valid.packages},
		},
		"missing specialization value": {
			{
				id: "specialization",
				placement: specializedPlacement(
					affinityDesired,
					roleSemanticKernel,
					specializationFamily,
					"",
				),
				packages: valid.packages,
			},
		},
		"value without specialization": {
			{
				id: "specialization",
				placement: packagePlacement{
					affinity: affinityDesired,
					role:     roleSemanticKernel,
					specialization: packageSpecialization{
						kind: specializationNone, value: "Skill",
					},
				},
				packages: valid.packages,
			},
		},
		"duplicate package": {
			valid,
			{id: "duplicate", placement: valid.placement, packages: valid.packages},
		},
		"malformed package path": {
			{id: "path", placement: valid.placement, packages: []string{"internal/desired/../effect"}},
		},
		"implicit descendant pattern": {
			{id: "path", placement: valid.placement, packages: []string{"internal/desired/**"}},
		},
		"non-internal package path": {
			{id: "path", placement: valid.placement, packages: []string{"cmd/daem"}},
		},
		"non-canonical separators": {
			{id: "path", placement: valid.placement, packages: []string{`internal\desired`}},
		},
		"non-canonical whitespace": {
			{id: "path", placement: valid.placement, packages: []string{" internal/desired"}},
		},
	}

	for name, rows := range tests {
		t.Run(name, func(t *testing.T) {
			findings := validatePackagePlacementRows(rows)
			if countViolationRule(findings, rulePackagePlacementMetadata) == 0 {
				t.Fatalf("metadata findings = %#v", findings)
			}
		})
	}
}

func TestPackagePlacementMetadataFindingsAreDeterministic(t *testing.T) {
	rows := []packagePlacementRow{
		{id: "z", placement: plainPlacement(affinityUnknown, roleUnknown), packages: []string{"internal/z"}},
		{id: "a", placement: plainPlacement(affinityUnknown, roleUnknown), packages: []string{"internal/a"}},
	}
	first := validatePackagePlacementRows(rows)
	second := validatePackagePlacementRows(rows)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("metadata findings are nondeterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
}
