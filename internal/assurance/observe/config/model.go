package config

import (
	"fmt"
	"strings"
)

// ActivationDisclosure reports an activation-like field observed in passive config.
type ActivationDisclosure string

const (
	ActivationConfiguredTrue  ActivationDisclosure = "configured_true"
	ActivationConfiguredFalse ActivationDisclosure = "configured_false"
	ActivationNotDeclared     ActivationDisclosure = "not_declared"
	ActivationUnsupportedType ActivationDisclosure = "unsupported_type"
)

// EntryState classifies one passive config entry.
type EntryState string

const (
	EntryObserved    EntryState = "observed"
	EntryUnsupported EntryState = "unsupported"
)

// EntrySetState classifies the config container that may hold entries.
type EntrySetState string

const (
	EntrySetNotDeclared EntrySetState = "not_declared"
	EntrySetObserved    EntrySetState = "observed"
	EntrySetUnsupported EntrySetState = "unsupported"
)

// ReasonCode is a stable passive config-entry unsupported reason.
type ReasonCode string

const (
	ReasonNone                 ReasonCode = ""
	ReasonEmptyEntryKey        ReasonCode = "empty_config_key"
	ReasonEntryNotTable        ReasonCode = "entry_not_table"
	ReasonActivationNotBoolean ReasonCode = "activation_not_boolean"
)

// Key is the host-visible config entry key.
type Key string

// EntrySpec contains already-decoded passive config entry facts.
type EntrySpec struct {
	Key        Key
	State      EntryState
	Activation ActivationDisclosure
	Reason     ReasonCode
}

// Entry is one passive config entry observation.
type Entry struct {
	key        Key
	state      EntryState
	activation ActivationDisclosure
	reason     ReasonCode
}

// NewEntry validates and constructs a passive config entry observation.
func NewEntry(spec EntrySpec) (Entry, error) {
	if !validActivationDisclosure(spec.Activation) {
		return Entry{}, fmt.Errorf("config entry %q has unsupported activation disclosure %q", spec.Key, spec.Activation)
	}
	switch spec.State {
	case EntryObserved:
		if strings.TrimSpace(string(spec.Key)) == "" {
			return Entry{}, fmt.Errorf("observed config entry key is required")
		}
		if spec.Activation == ActivationUnsupportedType {
			return Entry{}, fmt.Errorf("observed config entry %q cannot carry unsupported activation", spec.Key)
		}
		if spec.Reason != ReasonNone {
			return Entry{}, fmt.Errorf("observed config entry %q cannot carry unsupported reason %q", spec.Key, spec.Reason)
		}
	case EntryUnsupported:
		if !validReasonCode(spec.Reason) || spec.Reason == ReasonNone {
			return Entry{}, fmt.Errorf("unsupported config entry %q requires a supported reason", spec.Key)
		}
		if strings.TrimSpace(string(spec.Key)) == "" && spec.Reason != ReasonEmptyEntryKey {
			return Entry{}, fmt.Errorf("empty config entry key requires reason %q", ReasonEmptyEntryKey)
		}
		if spec.Reason == ReasonActivationNotBoolean && spec.Activation != ActivationUnsupportedType {
			return Entry{}, fmt.Errorf("activation-not-boolean config entry %q requires unsupported activation disclosure", spec.Key)
		}
	default:
		return Entry{}, fmt.Errorf("config entry %q has unsupported state %q", spec.Key, spec.State)
	}
	return Entry{
		key:        spec.Key,
		state:      spec.State,
		activation: spec.Activation,
		reason:     spec.Reason,
	}, nil
}

// Key returns the host-visible config entry key.
func (entry Entry) Key() Key { return entry.key }

// Activation returns the passive activation disclosure.
func (entry Entry) Activation() ActivationDisclosure { return entry.activation }

// Reason returns the unsupported reason, if the entry is unsupported.
func (entry Entry) Reason() ReasonCode { return entry.reason }

// Observed reports whether the entry is a supported passive observation.
func (entry Entry) Observed() bool { return entry.state == EntryObserved }

// Unsupported reports whether the entry shape was visible but unsupported.
func (entry Entry) Unsupported() bool { return entry.state == EntryUnsupported }

// ObservationSpec contains passive config-entry observation facts for one source.
type ObservationSpec struct {
	SourcePath    string
	ConfigExists  bool
	EntrySetState EntrySetState
	Entries       []Entry
}

// Observation records passive config-entry visibility for one source.
type Observation struct {
	configExists  bool
	entrySetState EntrySetState
	entries       []Entry
}

// NewObservation validates and constructs a passive config-entry observation.
func NewObservation(spec ObservationSpec) (Observation, error) {
	sourcePath := strings.TrimSpace(spec.SourcePath)
	if sourcePath == "" {
		return Observation{}, fmt.Errorf("config observation source path is required")
	}
	if !validEntrySetState(spec.EntrySetState) {
		return Observation{}, fmt.Errorf("config observation %q has unsupported entry-set state %q", sourcePath, spec.EntrySetState)
	}
	if !spec.ConfigExists && spec.EntrySetState != EntrySetNotDeclared {
		return Observation{}, fmt.Errorf("missing config observation %q cannot carry entry-set state %q", sourcePath, spec.EntrySetState)
	}
	if spec.EntrySetState != EntrySetObserved && len(spec.Entries) != 0 {
		return Observation{}, fmt.Errorf("config observation %q cannot carry entries when entry-set state is %q", sourcePath, spec.EntrySetState)
	}
	return Observation{
		configExists:  spec.ConfigExists,
		entrySetState: spec.EntrySetState,
		entries:       append([]Entry(nil), spec.Entries...),
	}, nil
}

// ConfigExists reports whether the source config file was present.
func (observation Observation) ConfigExists() bool { return observation.configExists }

// EntrySetObserved reports whether entries were decoded from a supported entry set.
func (observation Observation) EntrySetObserved() bool {
	return observation.entrySetState == EntrySetObserved
}

// EntrySetUnsupported reports whether the entry set was present but unsupported.
func (observation Observation) EntrySetUnsupported() bool {
	return observation.entrySetState == EntrySetUnsupported
}

// Entries returns a defensive copy of passive config entries.
func (observation Observation) Entries() []Entry {
	return append([]Entry(nil), observation.entries...)
}

func validActivationDisclosure(disclosure ActivationDisclosure) bool {
	switch disclosure {
	case ActivationConfiguredTrue,
		ActivationConfiguredFalse,
		ActivationNotDeclared,
		ActivationUnsupportedType:
		return true
	default:
		return false
	}
}

func validEntrySetState(state EntrySetState) bool {
	switch state {
	case EntrySetNotDeclared, EntrySetObserved, EntrySetUnsupported:
		return true
	default:
		return false
	}
}

func validReasonCode(reason ReasonCode) bool {
	switch reason {
	case ReasonNone,
		ReasonEmptyEntryKey,
		ReasonEntryNotTable,
		ReasonActivationNotBoolean:
		return true
	default:
		return false
	}
}
