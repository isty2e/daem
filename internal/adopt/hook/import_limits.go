package hook

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	targetpkg "github.com/isty2e/daem/internal/target"
)

type importHookEventIdentity struct {
	resourceEvent   string
	diagnosticToken string
}

type importHookCollector struct {
	target          targetpkg.Target
	scope           targetpkg.Scope
	livePath        string
	hooks           []adopt.Hook
	skipped         []adopt.Skipped
	diagnosticBytes int
	exceeded        bool
	usedNames       map[string]struct{}
	nextNameSuffix  map[string]int
}

func importHookName(
	target targetpkg.Target,
	scope targetpkg.Scope,
	event string,
	groupIndex int,
	handlerIndex int,
) string {
	return sanitizeImportHookName(fmt.Sprintf("%s_%s_%s_%d_%d", target, scope, event, groupIndex+1, handlerIndex+1))
}

func sanitizeImportHookName(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, item := range strings.ToLower(value) {
		if (item >= 'a' && item <= 'z') || (item >= '0' && item <= '9') {
			builder.WriteRune(item)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && builder.Len() != 0 {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	name := strings.Trim(builder.String(), "_")
	if name == "" {
		return "hook"
	}

	return name
}

func newImportHookEventIdentity(event string) importHookEventIdentity {
	return importHookEventIdentity{
		resourceEvent:   event,
		diagnosticToken: boundedImportHookToken(event),
	}
}

func (collector *importHookCollector) reserveHookName(
	identity importHookEventIdentity,
	groupIndex int,
	handlerIndex int,
) string {
	base := importHookName(collector.target, collector.scope, identity.resourceEvent, groupIndex, handlerIndex)
	if collector.usedNames == nil {
		collector.usedNames = make(map[string]struct{})
		collector.nextNameSuffix = make(map[string]int)
	}
	if _, used := collector.usedNames[base]; !used {
		collector.usedNames[base] = struct{}{}
		return base
	}

	suffix := collector.nextNameSuffix[base]
	if suffix < 2 {
		suffix = 2
	}
	for {
		candidate := fmt.Sprintf("%s_%d", base, suffix)
		suffix++
		if _, used := collector.usedNames[candidate]; used {
			continue
		}
		collector.nextNameSuffix[base] = suffix
		collector.usedNames[candidate] = struct{}{}
		return candidate
	}
}

func withinImportHookStructuralBudget(rawHooks map[string]json.RawMessage) bool {
	if len(rawHooks) > maximumImportHookEvents {
		return false
	}
	groupCount := 0
	handlerCount := 0
	for event, rawEvent := range rawHooks {
		if len(event) > maximumImportHookEventBytes {
			return false
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(rawEvent, &groups); err != nil {
			continue
		}
		if len(groups) > maximumImportHookGroups-groupCount {
			return false
		}
		groupCount += len(groups)
		for _, rawGroup := range groups {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawGroup, &fields); err != nil {
				continue
			}
			rawHandlers, ok := fields["hooks"]
			if !ok {
				continue
			}
			var handlers []json.RawMessage
			if err := json.Unmarshal(rawHandlers, &handlers); err != nil {
				continue
			}
			if len(handlers) > maximumImportHookHandlers-handlerCount {
				return false
			}
			handlerCount += len(handlers)
		}
	}
	return true
}

func boundedImportHookToken(value string) string {
	digest := sha256.Sum256([]byte(value))
	var builder strings.Builder
	lastUnderscore := false
	truncated := false
	for _, item := range strings.TrimSpace(value) {
		switch {
		case item >= 'a' && item <= 'z', item >= 'A' && item <= 'Z', item >= '0' && item <= '9', item == '-', item == '_', item == '.':
			if builder.Len() < 65 {
				builder.WriteRune(item)
			} else {
				truncated = true
			}
			lastUnderscore = item == '_'
		default:
			if builder.Len() != 0 && !lastUnderscore {
				if builder.Len() < 65 {
					builder.WriteByte('_')
				} else {
					truncated = true
				}
				lastUnderscore = true
			}
		}
	}
	prefix := strings.Trim(builder.String(), "_")
	if prefix == "" {
		return "unknown"
	}
	if !truncated && len(prefix) <= 64 {
		return prefix
	}
	return fmt.Sprintf("%s_%x", prefix[:32], digest[:8])
}

func importHookSkipReason(eventToken string, groupIndex int, handlerIndex int, reason string) string {
	return fmt.Sprintf("event=%s,group=%d,handler=%d,%s", eventToken, groupIndex+1, handlerIndex+1, reason)
}

func (collector *importHookCollector) addSkip(reason string) {
	if collector.exceeded {
		return
	}
	nextBytes := collector.diagnosticBytes + len(collector.livePath) + len(reason)
	if len(collector.skipped) >= maximumImportHookSkips || nextBytes > maximumImportHookDiagnosticBytes {
		collector.exceeded = true
		return
	}
	collector.diagnosticBytes = nextBytes
	collector.skipped = append(collector.skipped, adopt.Skipped{LivePath: collector.livePath, Reason: reason})
}

func importHookBudgetFailure(livePath string) ([]adopt.Hook, []adopt.Skipped) {
	return nil, []adopt.Skipped{{LivePath: livePath, Reason: importHookSkipBudgetExceeded}}
}
