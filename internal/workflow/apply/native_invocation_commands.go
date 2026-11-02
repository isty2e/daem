package apply

import (
	"encoding/json"
	"strings"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type nativeInvocationCommand struct {
	key         string
	rewrite     relationhost.NativeSettingsRewrites
	fingerprint string
	correlation bool
}

func nativeInvocationCommands(current commandPlan, removalAdapter executehostroute.RemovalAdapter, executor delegate.Executor) ([]nativeInvocationCommand, error) {
	var commands []nativeInvocationCommand
	appendCarrier := func(command executehostroute.Command, carrier desiredextension.CarrierKey, operation lock.OperationKind) error {
		rewrite, err := nativeInvocationRewrite(current, carrier, operation)
		if err != nil {
			return err
		}
		key, err := nativeInvocationCommandKey(command.AttemptRequest())
		if err != nil {
			return err
		}
		commands = append(commands, nativeInvocationCommand{key: key, rewrite: rewrite, fingerprint: strings.TrimPrefix(command.RouteRequest().CanonicalRequestHash(), "sha256:"), correlation: true})
		return nil
	}
	actions, err := providerInstallActions(current.assessment.MCPProviders)
	if err != nil {
		return nil, err
	}
	actions = append(actions, nonProviderRelationActions(current)...)
	for _, action := range actions {
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildCommand(executehostroute.BuildInput{Action: action, Lockfile: current.context.Lockfile, WorkDir: current.context.Paths.ManifestRoot})
		if err != nil {
			return nil, err
		}
		if err := appendCarrier(command, action.CarrierIdentity().Carrier().Key(), lock.OperationInstall); err != nil {
			return nil, err
		}
	}

	for _, action := range current.assessment.Reconciliation.CarrierAbsences() {
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildRemovalCommand(executehostroute.RemovalBuildInput{Action: action, WorkDir: current.context.Paths.ManifestRoot, Adapter: removalAdapter})
		if err != nil {
			return nil, err
		}
		if err := appendCarrier(command, action.Claim().Identity().Carrier().Key(), lock.OperationRemove); err != nil {
			return nil, err
		}
	}
	for _, action := range current.assessment.Reconciliation.Delegates() {
		request, scheduled := executor.ScheduledRequest(action)
		if !scheduled {
			continue
		}
		key, err := nativeDelegateInvocationKey(action, request)
		if err != nil {
			return nil, err
		}
		commands = append(commands, nativeInvocationCommand{key: key, fingerprint: strings.TrimPrefix(string(artifact.HashFileContent([]byte(key))), "sha256:")})
	}

	return commands, nil
}

func nativeDelegateInvocationKey(action reconcile.DelegateAction, request subprocess.CommandAttemptRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		Actor   []string
		Request subprocess.CommandAttemptRequest
	}{
		Actor: []string{string(action.Subject().Kind()), action.Subject().Namespace(), action.Subject().Key(), string(action.Target()), string(action.Scope()), action.Plan().IdentityKey()}, Request: request,
	})
	return "delegate:" + string(encoded), err
}
