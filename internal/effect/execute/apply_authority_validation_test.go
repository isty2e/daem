package execute

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestApplyAuthorityValidationPreservesSnapshotProtocol(t *testing.T) {
	ctx := context.Background()
	request := mutation.PhysicalAuthorityRequest{
		Path: filepath.Join(t.TempDir(), "value"), Target: "codex", Scope: "project",
	}
	authority := &mutationAuthority{physicalAuthorityRequests: []mutation.PhysicalAuthorityRequest{request}}
	declined := errors.New("snapshot declined")
	called := false
	options := ApplyOptions{ValidateBeforeEffects: func(got context.Context, _ mutation.PhysicalAuthoritySet) error {
		if got != ctx {
			t.Fatal("context changed")
		}
		called = true
		return declined
	}}
	if err := options.validatePhysicalAuthority(ctx, authority); !errors.Is(err, declined) || !called {
		t.Fatalf("snapshot validation: called=%t error=%v", called, err)
	}

	authority.physicalAuthorityRequests[0].Path = "\x00"
	called = false
	if err := options.validatePhysicalAuthority(ctx, authority); err == nil || called {
		t.Fatalf("invalid request reached snapshot callback: called=%t error=%v", called, err)
	}
	if err := (ApplyOptions{}).validatePhysicalAuthority(ctx, authority); err == nil {
		t.Fatal("standalone execution skipped physical observation")
	}
	authority.physicalAuthorityRequests[0] = request
	if err := (ApplyOptions{}).validatePhysicalAuthority(ctx, authority); err != nil {
		t.Fatalf("standalone physical observation: %v", err)
	}
}

func TestApplyAuthorityRequestValidationCopiesFreshInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authority := &mutationAuthority{physicalAuthorityRequests: []mutation.PhysicalAuthorityRequest{{
		Path: filepath.Join(t.TempDir(), "first"), Target: "codex", Scope: "project",
	}}}
	declined := errors.New("request validation declined")
	calls := 0
	options := ApplyOptions{ValidatePhysicalAuthorityRequests: func(got context.Context, requests []mutation.PhysicalAuthorityRequest) error {
		calls++
		if got != ctx || !reflect.DeepEqual(requests, authority.physicalAuthorityRequests) {
			t.Fatalf("request delivery: context=%v requests=%v want=%v", got, requests, authority.physicalAuthorityRequests)
		}
		requests[0].Path = "changed by callback"
		if authority.physicalAuthorityRequests[0].Path == requests[0].Path {
			t.Fatal("callback mutated retained effect requests")
		}
		if err := got.Err(); err != nil {
			return err
		}
		return declined
	}}
	for i := 0; i < 2; i++ {
		if err := options.validatePhysicalAuthority(ctx, authority); !errors.Is(err, declined) {
			t.Fatalf("request callback error: %v", err)
		}
		authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, mutation.PhysicalAuthorityRequest{
			Path: filepath.Join(t.TempDir(), "next"), Target: "claude-code", Scope: "project",
		})
	}
	cancel()
	if err := options.validatePhysicalAuthority(ctx, authority); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled callback: %v", err)
	}
	if calls != 3 {
		t.Fatalf("callback calls=%d, want 3", calls)
	}
}

func TestApplyAuthorityValidationRejectsConflictingProtocols(t *testing.T) {
	calls := 0
	options := ApplyOptions{
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			calls++
			return nil
		},
		ValidatePhysicalAuthorityRequests: func(context.Context, []mutation.PhysicalAuthorityRequest) error {
			calls++
			return nil
		},
	}
	if err := options.validatePhysicalAuthority(context.Background(), &mutationAuthority{}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") || calls != 0 {
		t.Fatalf("conflicting protocols: calls=%d error=%v", calls, err)
	}
	if err := options.validatePhysicalAuthority(context.Background(), nil); err == nil || calls != 0 {
		t.Fatalf("missing authority: calls=%d error=%v", calls, err)
	}
}
