package apply

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
)

type nativeInvocationCommand struct {
	command executehostroute.Command
	carrier desiredextension.CarrierKey
}

func nativeInvocationCommands(current commandPlan, removalAdapter executehostroute.RemovalAdapter) ([]nativeInvocationCommand, error) {
	var commands []nativeInvocationCommand
	for _, action := range current.assessment.Reconciliation.Relations() {
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildCommand(executehostroute.BuildInput{Action: action, Lockfile: current.context.Lockfile, WorkDir: current.context.Paths.ManifestRoot})
		if err != nil {
			return nil, err
		}
		commands = append(commands, nativeInvocationCommand{command: command, carrier: action.CarrierIdentity().Carrier().Key()})
	}

	for _, action := range current.assessment.Reconciliation.CarrierAbsences() {
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildRemovalCommand(executehostroute.RemovalBuildInput{Action: action, WorkDir: current.context.Paths.ManifestRoot, Adapter: removalAdapter})
		if err != nil {
			return nil, err
		}
		commands = append(commands, nativeInvocationCommand{command: command, carrier: action.Claim().Identity().Carrier().Key()})
	}

	return commands, nil
}
