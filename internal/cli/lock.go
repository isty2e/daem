package cli

import (
	"bytes"
	"errors"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func prepareLockInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"lock"}, stderr)
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "resolve and validate without writing the lockfile")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	plan := prepareGoArguments(flags, args)
	response := newCommandResponse("lock", plan.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"lock"}, stdout, 0)
			return 0
		}
		if err := plan.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"lock"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if err := validatePresentationFlags("lock", plan.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}

		var preparedJSON []byte
		var prepareResult func(workflowlock.Result) error
		if plan.output == requestedJSON {
			prepareResult = func(result workflowlock.Result) error {
				var err error
				preparedJSON, err = prepareLockResultJSON(result, *dryRun)
				return err
			}
		}

		progress := newLockProgressRenderer(plan.output == requestedJSON, stderr, options)
		result, err := workflowlock.RunLock(options.context, workflowlock.LockInput{
			ManifestPath:  *manifestPath,
			LockfilePath:  "",
			DryRun:        *dryRun,
			SourceEvents:  progress.SourceSink(),
			LockEvents:    progress.LockSink(),
			PrepareResult: prepareResult,
		})
		progress.Close()
		if err != nil {
			status := workflowlock.MetadataRecoveryStatusOf(err)
			diagnostic := humanFailureDiagnostic(err).WithPrefix("lock failed: ").
				WithGuidance(lockMetadataGuidance(*dryRun, status)...)
			if status == workflowlock.MetadataRecoveryNotApplicable || status == workflowlock.MetadataRecoveryRetired {
				diagnostic = diagnostic.WithGuidance(lockWorkflowGuidance(*manifestPath, err)...)
			}
			return response.failure(operationFailure, diagnostic)
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}

		if plan.output == requestedJSON {
			if _, err := io.Copy(stdout, bytes.NewReader(preparedJSON)); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("lock failed: write json: "))
			}
			return 0
		}

		summary := lockSummary(result, *dryRun)
		if *dryRun {
			summary.NextCommand = lockCommandHint(result.ManifestPath, *manifestPath)
		}
		clipresent.PrintLockSummary(stdout, summary, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}}
}

func lockSummary(result workflowlock.Result, dryRun bool) clipresent.LockSummaryInput {
	preview, written := "would write lockfile", "wrote lockfile"
	var unavailable string
	switch result.PreviousStatus() {
	case workflowlock.PreviousDamaged:
		preview, written = "would rebuild damaged lockfile", "rebuilt damaged lockfile"
		unavailable = "lock changes not compared: previous lockfile contains malformed TOML"
	case workflowlock.PreviousLegacy:
		preview, written = "would replace older-schema lockfile", "replaced older-schema lockfile"
		unavailable = "lock changes not compared: previous lockfile uses an older schema"
	}
	heading := written
	if dryRun {
		heading = preview
	}
	return clipresent.LockSummaryInput{
		Heading:      heading,
		LockfilePath: result.LockfilePath,
		Lockfile:     result.Lockfile,
		Delta:        result.Comparison(),
		Unavailable:  unavailable,
		DryRun:       dryRun,
	}
}

func lockMetadataGuidance(dryRun bool, status workflowlock.MetadataRecoveryStatus) []clipresent.Guidance {
	troubleshooting := clipresent.InstructionGuidance("see 'Manifest Metadata Update Was Interrupted' in docs/troubleshooting.md")
	switch status {
	case workflowlock.MetadataRecoveryPending:
		if dryRun {
			return []clipresent.Guidance{clipresent.NoteGuidance("preview leaves the recovery record untouched; if you intend to refresh this lockfile, daem lock with the same manifest selection can clear an already completed manifest and lockfile update")}
		}
		return []clipresent.Guidance{troubleshooting}
	case workflowlock.MetadataRecoveryDifferentPair:
		return []clipresent.Guidance{troubleshooting}
	case workflowlock.MetadataRecoveryUnfinishedPair:
		return []clipresent.Guidance{clipresent.InstructionGuidance("retry the exact interrupted authoring command with the same manifest selection and arguments")}
	case workflowlock.MetadataRecoveryRetired:
		return []clipresent.Guidance{clipresent.NoteGuidance("the previous manifest and lockfile recovery record was already cleared")}
	case workflowlock.MetadataRecoveryRetirementUnconfirmed:
		return []clipresent.Guidance{clipresent.InstructionGuidance("preserve any remaining recovery files for inspection; do not remove private files by name prefix")}
	}
	return nil
}

func lockWorkflowGuidance(manifestPath string, err error) []clipresent.Guidance {
	var guidance []clipresent.Guidance
	if initialize, present := missingManifestInitGuidance(manifestPath, err); present {
		guidance = append(guidance, initialize)
	}
	var commandError workflowlock.CommandError
	if errors.As(err, &commandError) {
		if source, present := clipresent.MissingSourceGuidance(err); present {
			guidance = append(
				guidance,
				source,
				manifestCommandGuidance([]string{"lock"}, commandError.ManifestPath, manifestHintOptions(manifestPath, false), "--dry-run"),
			)
		}
	}
	return guidance
}
