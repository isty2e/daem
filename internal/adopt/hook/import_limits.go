package hook

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	targetpkg "github.com/isty2e/daem/internal/target"
)

var errImportHookStructuralBudgetExceeded = errors.New("hook import structural budget exceeded")

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

type importHookStructuralBudget struct {
	events   int
	groups   int
	handlers int
}

func scanImportHookStructuralBudget(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return skipImportHookJSONValue(decoder, opening, 0)
	}
	if err := validateImportHookJSONDepth(0); err != nil {
		return err
	}

	budget := importHookStructuralBudget{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("hook document object key is not a string")
		}
		valueToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if key == "hooks" && valueToken == json.Delim('{') {
			if err := validateImportHookJSONDepth(1); err != nil {
				return err
			}
			if err := scanImportHookEvents(decoder, &budget, 1); err != nil {
				return err
			}
			continue
		}
		if err := skipImportHookJSONValue(decoder, valueToken, 1); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, '}')
}

func scanImportHookEvents(decoder *json.Decoder, budget *importHookStructuralBudget, objectDepth int) error {
	for decoder.More() {
		eventToken, err := decoder.Token()
		if err != nil {
			return err
		}
		event, ok := eventToken.(string)
		if !ok {
			return fmt.Errorf("hook event key is not a string")
		}
		budget.events++
		if budget.events > maximumImportHookEvents || len(event) > maximumImportHookEventBytes {
			return errImportHookStructuralBudgetExceeded
		}

		valueToken, err := decoder.Token()
		if err != nil {
			return err
		}
		valueDepth := objectDepth + 1
		if valueToken == json.Delim('[') {
			if err := validateImportHookJSONDepth(valueDepth); err != nil {
				return err
			}
			if err := scanImportHookGroups(decoder, budget, valueDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipImportHookJSONValue(decoder, valueToken, valueDepth); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, '}')
}

func scanImportHookGroups(decoder *json.Decoder, budget *importHookStructuralBudget, arrayDepth int) error {
	for decoder.More() {
		budget.groups++
		if budget.groups > maximumImportHookGroups {
			return errImportHookStructuralBudgetExceeded
		}
		groupToken, err := decoder.Token()
		if err != nil {
			return err
		}
		groupDepth := arrayDepth + 1
		if groupToken == json.Delim('{') {
			if err := validateImportHookJSONDepth(groupDepth); err != nil {
				return err
			}
			if err := scanImportHookGroup(decoder, budget, groupDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipImportHookJSONValue(decoder, groupToken, groupDepth); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, ']')
}

func scanImportHookGroup(decoder *json.Decoder, budget *importHookStructuralBudget, objectDepth int) error {
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("hook group object key is not a string")
		}
		valueToken, err := decoder.Token()
		if err != nil {
			return err
		}
		valueDepth := objectDepth + 1
		if key == "hooks" && valueToken == json.Delim('[') {
			if err := validateImportHookJSONDepth(valueDepth); err != nil {
				return err
			}
			if err := scanImportHookHandlers(decoder, budget, valueDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipImportHookJSONValue(decoder, valueToken, valueDepth); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, '}')
}

func scanImportHookHandlers(decoder *json.Decoder, budget *importHookStructuralBudget, arrayDepth int) error {
	for decoder.More() {
		budget.handlers++
		if budget.handlers > maximumImportHookHandlers {
			return errImportHookStructuralBudgetExceeded
		}
		handlerToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if err := skipImportHookJSONValue(decoder, handlerToken, arrayDepth+1); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, ']')
}

func skipImportHookJSONValue(decoder *json.Decoder, firstToken json.Token, depth int) error {
	if err := validateImportHookJSONDepth(depth); err != nil {
		return err
	}
	delimiter, composite := firstToken.(json.Delim)
	if !composite {
		return nil
	}
	if delimiter != json.Delim('{') && delimiter != json.Delim('[') {
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}

	object := delimiter == json.Delim('{')
	closing := json.Delim(']')
	if object {
		closing = json.Delim('}')
	}
	for decoder.More() {
		if err := validateImportHookJSONDepth(depth + 1); err != nil {
			return err
		}
		if object {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			if _, ok := keyToken.(string); !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
		}
		valueToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if err := skipImportHookJSONValue(decoder, valueToken, depth+1); err != nil {
			return err
		}
	}
	return consumeImportHookJSONClosing(decoder, closing)
}

func validateImportHookJSONDepth(depth int) error {
	if depth <= hookdocument.MaximumDepth {
		return nil
	}
	return fmt.Errorf(
		"%w: maximum=%d",
		jsonstrict.ErrMaximumDepthExceeded,
		hookdocument.MaximumDepth,
	)
}

func consumeImportHookJSONClosing(decoder *json.Decoder, expected json.Delim) error {
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != expected {
		return fmt.Errorf("unexpected JSON delimiter %v, want %q", closing, expected)
	}
	return nil
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
