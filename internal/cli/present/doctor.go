package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/findings"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type DoctorJSONInput struct {
	ManifestPath     string
	ManifestExplicit bool
	Selection        targetselection.Selection
	Checks           []findings.Check
}

type doctorJSONOutput struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	Manifest      doctorJSONManifest `json:"manifest"`
	Targets       []string           `json:"targets"`
	CheckCount    int                `json:"check_count"`
	HasErrors     bool               `json:"has_errors"`
	Checks        []doctorJSONCheck  `json:"checks"`
}

type doctorJSONManifest struct {
	Path     string `json:"path"`
	Explicit bool   `json:"explicit"`
}

type doctorJSONCheck struct {
	Status        string   `json:"status"`
	Name          string   `json:"name"`
	Detail        string   `json:"detail"`
	Repairability string   `json:"repairability,omitempty"`
	RepairActions []string `json:"repair_actions,omitempty"`
	ManualReasons []string `json:"manual_reasons,omitempty"`
	NextStep      string   `json:"next_step,omitempty"`
}

func PrintDoctorChecksWithOptions(output io.Writer, checks []findings.Check, options HumanOptions) {
	okCount, warnCount, errorCount, skippedCount, unsupportedCount := 0, 0, 0, 0, 0
	for _, check := range checks {
		switch check.Status {
		case findings.CheckOK:
			okCount++
		case findings.CheckWarn:
			warnCount++
		case findings.CheckError:
			errorCount++
		case findings.CheckSkipped:
			skippedCount++
		case findings.CheckUnsupported:
			unsupportedCount++
		}
	}
	fmt.Fprintf(
		output,
		"doctor: %d checks (ok=%d warn=%d error=%d skipped=%d unsupported=%d)\n",
		len(checks),
		okCount,
		warnCount,
		errorCount,
		skippedCount,
		unsupportedCount,
	)
	for _, check := range checks {
		if !options.Verbose && check.Status == findings.CheckOK {
			continue
		}
		fmt.Fprintf(output, "%s %s", check.Status, check.Name)
		if check.Detail != "" && (options.Verbose || check.Status != findings.CheckOK) {
			fmt.Fprintf(output, " detail=%q", check.Detail)
		}
		if check.Repairability != "" {
			fmt.Fprintf(output, " repairability=%s", check.Repairability)
		}
		if check.NextStep != "" {
			fmt.Fprintf(output, " next=%q", check.NextStep)
		}
		if options.Verbose && len(check.RepairActions) != 0 {
			fmt.Fprintf(output, " repair_actions=%q", check.RepairActions)
		}
		if options.Verbose && len(check.ManualReasons) != 0 {
			fmt.Fprintf(output, " manual_reasons=%q", check.ManualReasons)
		}
		fmt.Fprintln(output)
	}
}

func PrintDoctorJSON(output io.Writer, input DoctorJSONInput) error {
	payload := doctorJSONOutput{
		SchemaVersion: contractversion.DoctorJSON,
		Command:       "doctor",
		Manifest: doctorJSONManifest{
			Path:     input.ManifestPath,
			Explicit: input.ManifestExplicit,
		},
		Targets:    doctorJSONTargets(input.Selection),
		CheckCount: len(input.Checks),
		HasErrors:  findings.HasCheckErrors(input.Checks),
		Checks:     doctorJSONChecks(input.Checks),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func doctorJSONTargets(selection targetselection.Selection) []string {
	targets := selection.Targets()
	result := make([]string, 0, len(targets))
	for _, target := range targets {
		result = append(result, string(target))
	}

	return result
}

func doctorJSONChecks(checks []findings.Check) []doctorJSONCheck {
	result := make([]doctorJSONCheck, 0, len(checks))
	for _, check := range checks {
		result = append(result, doctorJSONCheck{
			Status:        string(check.Status),
			Name:          check.Name,
			Detail:        check.Detail,
			Repairability: check.Repairability,
			RepairActions: append([]string(nil), check.RepairActions...),
			ManualReasons: append([]string(nil), check.ManualReasons...),
			NextStep:      check.NextStep,
		})
	}

	return result
}
