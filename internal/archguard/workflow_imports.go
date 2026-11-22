package archguard

import "strings"

type workflowRoles uint8

const (
	nativeInputs workflowRoles = 1 << iota
	workspaceAssessment
	forwardInvocation
	lockGeneration
	workspaceCleanup
	protectionRecovery
)

func commandRoles(command string) workflowRoles {
	switch command {
	case "adopt":
		return nativeInputs
	case "apply":
		return workspaceAssessment | forwardInvocation | workspaceCleanup
	case "authoring":
		return lockGeneration
	case "list", "status":
		return workspaceAssessment
	case "lock":
		return nativeInputs | lockGeneration | workspaceCleanup
	case "recover":
		return nativeInputs | protectionRecovery
	case "refresh":
		return nativeInputs | forwardInvocation
	}
	return 0
}

func (roles workflowRoles) allows(service string) bool {
	switch service {
	case "nativeconfig":
		return roles&(nativeInputs|workspaceAssessment) != 0
	case "readiness":
		return roles&workspaceAssessment != 0
	case "nativeconfig/invocation", "protection/invocation":
		return roles&forwardInvocation != 0
	case "lock/generate":
		return roles&lockGeneration != 0
	case "workspacecleanup":
		return roles&workspaceCleanup != 0
	case "protection/recovery":
		return roles&protectionRecovery != 0
	}
	return false
}

func isForbiddenNestedWorkflowImport(packagePath, importPath string) bool {
	if !isWorkflowPackage(packagePath) || !isWorkflowPackage(importPath) {
		return false
	}

	const prefix = "internal/workflow/"
	from, to := strings.TrimPrefix(packagePath, prefix), strings.TrimPrefix(importPath, prefix)
	switch from {
	case "nativeconfig":
		return to != "readiness"
	case "workspacecleanup":
		return to != "nativeconfig"
	case "nativeconfig/invocation":
		return to != "nativeconfig" && to != "protection/invocation"
	default:
		return !commandRoles(from).allows(to)
	}
}
