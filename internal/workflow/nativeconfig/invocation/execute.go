// Package invocation owns one configuration command's reserved reads,
// permission-protection transition and before/after document qualification.
// Its caller owns the command schedule, state publication and observation roots.
package invocation

import (
	"context"
	"errors"
	"fmt"
	"time"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	permissioninvocation "github.com/isty2e/daem/internal/workflow/protection/invocation"
)

var ErrUnavailable = errors.New("native command is not available in its reserved invocation schedule")

// CompletionContext preserves the containing workflow's post-observation policy.
// Its zero value follows caller cancellation.
type CompletionContext struct{ window time.Duration }

func CallerContext() CompletionContext { return CompletionContext{} }

func CompletionWindow(window time.Duration) (CompletionContext, error) {
	if window <= 0 {
		return CompletionContext{}, fmt.Errorf("completion observation window must be positive")
	}
	return CompletionContext{window: window}, nil
}

func (completion CompletionContext) after(ctx context.Context) (context.Context, context.CancelFunc) {
	if completion.window == 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), completion.window)
}

type Invocation struct {
	documents  nativeconfig.InvocationDocuments
	reads      physicalwork.InvocationWorkReservation
	rewrites   relationhost.NativeSettingsRewrites
	protection *permissioninvocation.InvocationLayout
	completion CompletionContext
	used       bool
}

func New(documents nativeconfig.InvocationDocuments, reads physicalwork.InvocationWorkReservation, rewrites relationhost.NativeSettingsRewrites, protection *permissioninvocation.InvocationLayout, completion CompletionContext) *Invocation {
	return &Invocation{documents: documents, reads: reads, rewrites: rewrites, protection: protection, completion: completion}
}

// Execute consumes the prepared invocation once. Correlation publishes only the
// containing operation's pre-command state, using its already reserved work.
// An observed attempt remains available even when post-observation fails.
func (invocation *Invocation) Execute(ctx context.Context, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, correlation func(context.Context, *physicalwork.Budget) error) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
	if invocation == nil || invocation.used {
		return attempt, false, ErrUnavailable
	}
	invocation.used = true
	reads, _, err := invocation.reads.Take()
	if err != nil {
		return attempt, false, err
	}
	var before nativeconfig.InvocationBaseline
	prepared := false
	prepare := func(ctx context.Context, budget *physicalwork.Budget) error {
		var err error
		before, err = invocation.documents.Read(ctx, reads)
		if err != nil {
			return err
		}
		if err := invocation.documents.Qualify(ctx, before, before, relationhost.NativeSettingsRewrites{}); err != nil {
			return err
		}
		prepared = true
		if correlation != nil {
			return correlation(ctx, budget)
		}
		return nil
	}
	if invocation.protection != nil {
		input, err := invocation.protection.AttachProtection(ctx)
		if err != nil {
			return attempt, false, err
		}
		result, err := permissioninvocation.ExecuteInvocation(ctx, input, executor, request, bind, prepare)
		returnErr = err
		if process, present := result.Process(); present {
			attempt, observed = process.Attempt(), true
		}
	} else {
		if err := prepare(ctx, reads); err != nil {
			return attempt, false, err
		}
		attempt, observed = executor.ExecuteInWorkingDirectory(ctx, request, bind), true
	}
	if observed && prepared {
		postCtx, cancel := invocation.completion.after(ctx)
		defer cancel()
		after, err := invocation.documents.Read(postCtx, reads)
		if err == nil {
			err = invocation.documents.Qualify(postCtx, before, after, invocation.rewrites)
		}
		returnErr = errors.Join(returnErr, err)
	}
	return attempt, observed, returnErr
}
