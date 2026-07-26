package hostroute

import (
	"fmt"
	"slices"
	"strings"
)

const (
	InvocationKindCommand = "command"
	CWDPolicySelectedRoot = "selected_project_root"
)

// DisclosureInput contains adapter-owned, secret-free facts that must be
// disclosed before one host route may execute.
type DisclosureInput struct {
	ExecutionSubject      string
	InvocationKind        string
	CWDPolicy             string
	TimeoutSeconds        int
	EffectClasses         []string
	RetainedEffectClasses []string
	NonClaims             []string
}

// Disclosure is the immutable adapter-owned effect envelope for one command.
// It carries no argv, current observation, user authorization, or execution
// authority.
type Disclosure struct {
	executionSubject      string
	invocationKind        string
	cwdPolicy             string
	timeoutSeconds        int
	effectClasses         []string
	retainedEffectClasses []string
	nonClaims             []string
}

// NewDisclosure validates and constructs one deterministic effect disclosure.
func NewDisclosure(input DisclosureInput) (Disclosure, error) {
	disclosure := Disclosure{
		executionSubject:      strings.TrimSpace(input.ExecutionSubject),
		invocationKind:        strings.TrimSpace(input.InvocationKind),
		cwdPolicy:             strings.TrimSpace(input.CWDPolicy),
		timeoutSeconds:        input.TimeoutSeconds,
		effectClasses:         canonicalDisclosureValues(input.EffectClasses),
		retainedEffectClasses: canonicalDisclosureValues(input.RetainedEffectClasses),
		nonClaims:             canonicalDisclosureValues(input.NonClaims),
	}
	if err := disclosure.validate(); err != nil {
		return Disclosure{}, err
	}
	return disclosure, nil
}

func (disclosure Disclosure) validate() error {
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "execution subject", value: disclosure.executionSubject},
		{label: "invocation kind", value: disclosure.invocationKind},
		{label: "cwd policy", value: disclosure.cwdPolicy},
	} {
		if err := validateDisclosureText(field.label, field.value); err != nil {
			return err
		}
	}
	if disclosure.invocationKind != InvocationKindCommand {
		return fmt.Errorf("host route invocation kind %q is unsupported", disclosure.invocationKind)
	}
	if disclosure.cwdPolicy != CWDPolicySelectedRoot {
		return fmt.Errorf("host route cwd policy %q is unsupported", disclosure.cwdPolicy)
	}
	if disclosure.timeoutSeconds <= 0 {
		return fmt.Errorf("host route timeout must be positive")
	}
	for _, set := range []struct {
		label  string
		values []string
	}{
		{label: "effect class", values: disclosure.effectClasses},
		{label: "retained effect class", values: disclosure.retainedEffectClasses},
		{label: "non-claim", values: disclosure.nonClaims},
	} {
		if len(set.values) == 0 {
			return fmt.Errorf("host route %s set is required", set.label)
		}
		for _, value := range set.values {
			if err := validateDisclosureText(set.label, value); err != nil {
				return err
			}
		}
		if !slices.IsSorted(set.values) {
			return fmt.Errorf("host route %s set must be sorted", set.label)
		}
		for index := 1; index < len(set.values); index++ {
			if set.values[index-1] == set.values[index] {
				return fmt.Errorf("host route %s set must be unique", set.label)
			}
		}
	}
	return nil
}

func validateDisclosureText(label string, value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("host route %s must be non-empty and trimmed", label)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return fmt.Errorf("host route %s must not contain control characters", label)
		}
	}
	return nil
}

func canonicalDisclosureValues(values []string) []string {
	result := append([]string(nil), values...)
	for index := range result {
		result[index] = strings.TrimSpace(result[index])
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (disclosure Disclosure) ExecutionSubject() string { return disclosure.executionSubject }
func (disclosure Disclosure) InvocationKind() string   { return disclosure.invocationKind }
func (disclosure Disclosure) CWDPolicy() string        { return disclosure.cwdPolicy }
func (disclosure Disclosure) TimeoutSeconds() int      { return disclosure.timeoutSeconds }
func (disclosure Disclosure) EffectClasses() []string {
	return append([]string(nil), disclosure.effectClasses...)
}

func (disclosure Disclosure) RetainedEffectClasses() []string {
	return append([]string(nil), disclosure.retainedEffectClasses...)
}

func (disclosure Disclosure) NonClaims() []string {
	return append([]string(nil), disclosure.nonClaims...)
}
