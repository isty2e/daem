// Package delegate defines canonical delegated executable invocation identity.
//
// The package is intentionally effect-free. It models the locked request that
// later planning or execution layers may consume, but it does not discover
// executables, invoke package managers, probe runtime readiness, or render host
// configuration.
package delegate
