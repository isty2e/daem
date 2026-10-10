package profile

import "errors"

var (
	ErrPiNativeVersionUnqualified = errors.New("native Pi MCP requires an observable stable Pi version >=1.0.2,<2.0.0; the recorded backend was not changed")
	ErrPiNativeBuiltinDisabled    = errors.New("native Pi MCP builtin is disabled in the applicable settings envelope; no settings were changed")
	ErrPiNativeBuiltinUnobserved  = errors.New("native Pi MCP builtin selection cannot be established within the static selector envelope; no settings were changed")
	ErrPiNativeAdapterConfigured  = errors.New("configured pi-mcp-adapter prevents Native qualification regardless of package filters; remove its declaration separately; no backend fallback was performed")
)
