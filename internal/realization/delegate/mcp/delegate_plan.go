package mcp

import (
	"errors"
	"fmt"
	"sort"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
)

// MCPDelegatePlanReasonCode is a stable reason for MCP delegate-plan lowering failures.
type MCPDelegatePlanReasonCode string

const (
	MCPDelegatePlanReasonNone                MCPDelegatePlanReasonCode = ""
	MCPDelegatePlanReasonUnsupportedServer   MCPDelegatePlanReasonCode = "UNSUPPORTED_SERVER"
	MCPDelegatePlanReasonMissingPackage      MCPDelegatePlanReasonCode = "MISSING_PACKAGE"
	MCPDelegatePlanReasonInvalidCommand      MCPDelegatePlanReasonCode = "INVALID_COMMAND"
	MCPDelegatePlanReasonInvalidEnvReference MCPDelegatePlanReasonCode = "INVALID_ENV_REFERENCE"
	MCPDelegatePlanReasonInvalidPackage      MCPDelegatePlanReasonCode = "INVALID_PACKAGE"
	MCPDelegatePlanReasonInvalidPlan         MCPDelegatePlanReasonCode = "INVALID_PLAN"
)

// MCPDelegatePlanError reports why an MCP server cannot produce a delegate plan.
type MCPDelegatePlanError struct {
	reason  MCPDelegatePlanReasonCode
	subject string
	message string
	cause   error
}

func newMCPDelegatePlanError(reason MCPDelegatePlanReasonCode, subject string, message string, cause error) *MCPDelegatePlanError {
	return &MCPDelegatePlanError{
		reason:  reason,
		subject: subject,
		message: message,
		cause:   cause,
	}
}

// Error implements error.
func (err *MCPDelegatePlanError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.subject == "" {
		return fmt.Sprintf("%s: %s", err.reason, err.message)
	}
	return fmt.Sprintf("%s: %s: %s", err.reason, err.subject, err.message)
}

// Unwrap returns the lower-level validation cause when one is available.
func (err *MCPDelegatePlanError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// Code returns the stable MCP delegate-plan lowering reason.
func (err *MCPDelegatePlanError) Code() MCPDelegatePlanReasonCode {
	if err == nil {
		return MCPDelegatePlanReasonNone
	}
	return err.reason
}

// Subject returns the invalid MCP delegate-plan subject when one is available.
func (err *MCPDelegatePlanError) Subject() string {
	if err == nil {
		return ""
	}
	return err.subject
}

// MCPBindingDelegatePlan lowers one canonical MCP binding command into delegate identity.
func MCPBindingDelegatePlan(server desiredmcp.Server, binding desiredmcp.Binding) (delegate.DelegatePlan, error) {
	if err := validateSupportedDelegateBinding(server, binding, "mcp_server.binding"); err != nil {
		return delegate.DelegatePlan{}, newMCPDelegatePlanError(
			MCPDelegatePlanReasonUnsupportedServer,
			server.ID().Name(),
			"unsupported MCP server for delegated executable plan",
			err,
		)
	}
	stdio, _ := binding.Transport().Stdio()
	return MCPStdioDelegatePlan(stdio)
}

// MCPStdioDelegatePlan derives exact invocation identity without deciding
// whether any target/scope binding admits delegated execution.
func MCPStdioDelegatePlan(stdio desiredmcp.Stdio) (delegate.DelegatePlan, error) {
	commandReference := stdio.Command()
	command, err := delegate.NewCommandSpec(commandReference.Executable(), stdio.Args())
	if err != nil {
		return delegate.DelegatePlan{}, newMCPDelegatePlanError(
			MCPDelegatePlanReasonInvalidCommand,
			commandReference.Executable(),
			"invalid MCP command argv for delegated executable plan",
			err,
		)
	}
	env, err := mcpDelegateEnvBindings(stdio.Env())
	if err != nil {
		return delegate.DelegatePlan{}, err
	}
	runner, err := mcpDelegateRunner(command)
	if err != nil {
		return delegate.DelegatePlan{}, err
	}

	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:  runner,
		Command: command,
		Env:     env,
	})
	if err != nil {
		var validation *delegate.ValidationError
		if errors.As(err, &validation) {
			switch validation.Code() {
			case delegate.ReasonMissingPackage:
				return delegate.DelegatePlan{}, newMCPDelegatePlanError(
					MCPDelegatePlanReasonMissingPackage,
					command.Executable(),
					"package-backed MCP delegate command requires a package argument",
					err,
				)
			case delegate.ReasonInvalidPackageRef:
				return delegate.DelegatePlan{}, newMCPDelegatePlanError(
					MCPDelegatePlanReasonInvalidPackage,
					validation.Subject(),
					"invalid delegated package reference",
					err,
				)
			}
		}
		return delegate.DelegatePlan{}, newMCPDelegatePlanError(
			MCPDelegatePlanReasonInvalidPlan,
			command.Executable(),
			"invalid delegated executable plan",
			err,
		)
	}
	return plan, nil
}

// MCPBindingDelegatePlanIfSupported returns the exact launcher plan when
// the selected host surface admits one as locked desired state.
func MCPBindingDelegatePlanIfSupported(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
) (delegate.DelegatePlan, bool, error) {
	if err := server.Validate(); err != nil {
		return delegate.DelegatePlan{}, false, err
	}
	if !server.OwnsBinding(binding) {
		return delegate.DelegatePlan{}, false, fmt.Errorf("MCP binding is not owned by server %q", server.ID().Name())
	}
	if _, err := aggregate.MCPPlacementForBinding(binding); err != nil {
		return delegate.DelegatePlan{}, false, err
	}
	if binding.Target() != target.TargetClaudeCode || binding.Scope() != target.ScopeProject {
		return delegate.DelegatePlan{}, false, nil
	}
	plan, err := MCPBindingDelegatePlan(server, binding)
	if err != nil {
		return delegate.DelegatePlan{}, false, err
	}
	return plan, true, nil
}

func validateSupportedDelegateBinding(server desiredmcp.Server, binding desiredmcp.Binding, context string) error {
	if err := server.Validate(); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if _, err := aggregate.MCPPlacementForBinding(binding); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if !server.OwnsBinding(binding) {
		return fmt.Errorf("%s: binding is not owned by MCP server %q", context, server.ID().Name())
	}
	if binding.Target() != target.TargetClaudeCode || binding.Scope() != target.ScopeProject {
		return fmt.Errorf("%s: delegated executable plan is admitted only for Claude Code project MCP, got %s/%s", context, binding.Target(), binding.Scope())
	}
	return nil
}

func mcpDelegateEnvBindings(env map[string]desiredmcp.EnvReference) (delegate.EnvBindingSet, error) {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	bindings := make([]delegate.EnvBinding, 0, len(keys))
	for _, key := range keys {
		binding, err := delegate.NewEnvBinding(key, env[key].FromEnv())
		if err != nil {
			return delegate.EnvBindingSet{}, newMCPDelegatePlanError(
				MCPDelegatePlanReasonInvalidEnvReference,
				key,
				"invalid delegated env binding",
				err,
			)
		}
		bindings = append(bindings, binding)
	}
	envBindings, err := delegate.NewEnvBindingSet(bindings)
	if err != nil {
		return delegate.EnvBindingSet{}, newMCPDelegatePlanError(
			MCPDelegatePlanReasonInvalidEnvReference,
			"env",
			"invalid delegated env binding",
			err,
		)
	}
	return envBindings, nil
}

func mcpDelegateRunner(command delegate.CommandSpec) (delegate.Runner, error) {
	var kind delegate.RunnerKind
	switch command.Executable() {
	case "npx":
		kind = delegate.RunnerNPX
	case "uvx":
		kind = delegate.RunnerUVX
	case "docker":
		kind = delegate.RunnerDocker
	default:
		kind = delegate.RunnerPlain
	}
	return delegate.NewRunner(kind)
}
