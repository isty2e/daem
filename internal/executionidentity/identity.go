// Package executionidentity models invocation and execution-context observations.
// These values do not establish live process ownership or quiescence.
package executionidentity

import (
	"fmt"
	"strings"
)

// Invocation identifies one freshly allocated command invocation, not a retry.
type Invocation struct{ value string }

func NewInvocation(value string) (Invocation, error) {
	if !canonicalHex(value, 32) {
		return Invocation{}, fmt.Errorf("invocation identity requires 32 nonzero lowercase hexadecimal digits")
	}
	return Invocation{value: value}, nil
}

func (invocation Invocation) Validate() error {
	_, err := NewInvocation(invocation.value)
	return err
}

func (invocation Invocation) String() string { return invocation.value }

// Platform selects the execution-context grammar, not the current runtime.
type Platform string

const (
	Darwin Platform = "darwin"
	Linux  Platform = "linux"
)

// Context retains independent stable-realm, boot and process-visibility facts.
// Earlier unqualified records have no visibility; only platform adapters can
// qualify how any of these observations were obtained.
type Context struct {
	platform   Platform
	realm      string
	epoch      string
	visibility string
}

// NewContext reconstructs the earlier unqualified context without inventing a
// process-visibility fact. Such a value is not comparable for group observation.
func NewContext(platform Platform, realm, epoch string) (Context, error) {
	if platform != Darwin && platform != Linux {
		return Context{}, fmt.Errorf("unsupported execution identity platform %q", platform)
	}
	if !canonicalHex(realm, 64) {
		return Context{}, fmt.Errorf("execution realm requires a nonzero lowercase SHA-256 digest")
	}
	if len(epoch) != 36 || epoch[8] != '-' || epoch[13] != '-' || epoch[18] != '-' || epoch[23] != '-' ||
		!canonicalHex(strings.ReplaceAll(epoch, "-", ""), 32) {
		return Context{}, fmt.Errorf("execution epoch requires a nonzero canonical lowercase UUID")
	}
	return Context{platform: platform, realm: realm, epoch: epoch}, nil
}

func NewContextWithVisibility(platform Platform, realm, epoch, visibility string) (Context, error) {
	value, err := NewContext(platform, realm, epoch)
	if err != nil {
		return Context{}, err
	}
	if !canonicalHex(visibility, 64) {
		return Context{}, fmt.Errorf("execution visibility requires a nonzero lowercase SHA-256 digest")
	}
	value.visibility = visibility
	return value, nil
}

func (context Context) Validate() error {
	if context.HasVisibility() {
		_, err := NewContextWithVisibility(context.platform, context.realm, context.epoch, context.visibility)
		return err
	}
	_, err := NewContext(context.platform, context.realm, context.epoch)
	return err
}

func (context Context) Platform() Platform  { return context.platform }
func (context Context) Realm() string       { return context.realm }
func (context Context) Epoch() string       { return context.epoch }
func (context Context) Visibility() string  { return context.visibility }
func (context Context) HasVisibility() bool { return context.visibility != "" }

type ContextRelation uint8

const (
	Incomparable ContextRelation = iota
	SameEpoch
	DifferentEpoch
)

// Relation compares recorded facts only. DifferentEpoch is not a certificate
// of local restart, and SameEpoch says nothing about group occupancy.
func (context Context) Relation(other Context) ContextRelation {
	if context.Validate() != nil || other.Validate() != nil ||
		!context.HasVisibility() || !other.HasVisibility() ||
		context.platform != other.platform || context.realm != other.realm {
		return Incomparable
	}
	if context.epoch == other.epoch {
		if context.visibility != other.visibility {
			return Incomparable
		}
		return SameEpoch
	}
	return DifferentEpoch
}

// Binding correlates one invocation with a dedicated process-group observation.
type Binding struct {
	invocation Invocation
	context    Context
	group      int64
}

func NewBinding(invocation Invocation, context Context, group int64) (Binding, error) {
	if err := invocation.Validate(); err != nil {
		return Binding{}, err
	}
	if err := context.Validate(); err != nil {
		return Binding{}, err
	}
	if group <= 0 || group > 1<<31-1 {
		return Binding{}, fmt.Errorf("execution process group must be a positive signed 32-bit identifier")
	}
	return Binding{invocation: invocation, context: context, group: group}, nil
}

func (binding Binding) Validate() error {
	_, err := NewBinding(binding.invocation, binding.context, binding.group)
	return err
}

func (binding Binding) Invocation() Invocation { return binding.invocation }
func (binding Binding) Context() Context       { return binding.context }
func (binding Binding) Group() int64           { return binding.group }

func canonicalHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	nonzero := false
	for index := range len(value) {
		character := value[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
		nonzero = nonzero || character != '0'
	}
	return nonzero
}
