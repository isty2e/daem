// Package nativeconfig owns command-local native input/profile composition.
// Reconciliation belongs to readiness; prospective lock generation does not
// import this observation boundary.
package nativeconfig

import (
	"context"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/toml"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func DocumentLimits() configdocument.Limits {
	return configdocument.Limits{
		MaximumBytes: recovery.MaximumRecoveryBackupFileBytes,
		MaximumDepth: tomlsyntax.MaximumDepth,
	}
}

func ResolveSourceEpoch(ctx context.Context, paths daempaths.Paths, environment desired.Environment, file lock.File, selection targetselection.Selection) (lockobserve.SourceEpoch, error) {
	if file.ContainsHostConfig() {
		return lockobserve.ResolveNativeDocumentSourceEpoch(ctx, paths, environment, file, selection, DocumentLimits())
	}
	return lockobserve.ResolveSourceEpoch(ctx, paths, environment, file, selection)
}
