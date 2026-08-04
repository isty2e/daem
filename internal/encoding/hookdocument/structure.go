package hookdocument

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

type structuralBudget struct {
	events   int
	groups   int
	handlers int
}

func validateDocumentStructure(content []byte) error {
	return validateDocumentStructureReader(bytes.NewReader(content))
}

func validateDocumentStructureReader(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return skipJSONValue(decoder, opening, 0)
	}
	if err := validateDepth(0); err != nil {
		return err
	}

	budget := structuralBudget{}
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
			if err := validateDepth(1); err != nil {
				return err
			}
			if err := scanEvents(decoder, &budget, 1); err != nil {
				return err
			}
			continue
		}
		if err := skipJSONValue(decoder, valueToken, 1); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, '}')
}

func validateProjectionStructure(content []byte) error {
	return validateProjectionStructureReader(bytes.NewReader(content))
}

func validateProjectionStructureReader(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return skipJSONValue(decoder, opening, 0)
	}
	if err := validateDepth(0); err != nil {
		return err
	}
	return scanEvents(decoder, &structuralBudget{}, 0)
}

func scanEvents(decoder *json.Decoder, budget *structuralBudget, objectDepth int) error {
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
		if budget.events > MaximumEvents || len(event) > MaximumEventBytes {
			return ErrStructuralBudgetExceeded
		}

		valueToken, err := decoder.Token()
		if err != nil {
			return err
		}
		valueDepth := objectDepth + 1
		if valueToken == json.Delim('[') {
			if err := validateDepth(valueDepth); err != nil {
				return err
			}
			if err := scanGroups(decoder, budget, valueDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipJSONValue(decoder, valueToken, valueDepth); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, '}')
}

func scanGroups(decoder *json.Decoder, budget *structuralBudget, arrayDepth int) error {
	for decoder.More() {
		budget.groups++
		if budget.groups > MaximumGroups {
			return ErrStructuralBudgetExceeded
		}
		groupToken, err := decoder.Token()
		if err != nil {
			return err
		}
		groupDepth := arrayDepth + 1
		if groupToken == json.Delim('{') {
			if err := validateDepth(groupDepth); err != nil {
				return err
			}
			if err := scanGroup(decoder, budget, groupDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipJSONValue(decoder, groupToken, groupDepth); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, ']')
}

func scanGroup(decoder *json.Decoder, budget *structuralBudget, objectDepth int) error {
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
			if err := validateDepth(valueDepth); err != nil {
				return err
			}
			if err := scanHandlers(decoder, budget, valueDepth); err != nil {
				return err
			}
			continue
		}
		if err := skipJSONValue(decoder, valueToken, valueDepth); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, '}')
}

func scanHandlers(decoder *json.Decoder, budget *structuralBudget, arrayDepth int) error {
	for decoder.More() {
		budget.handlers++
		if budget.handlers > MaximumHandlers {
			return ErrStructuralBudgetExceeded
		}
		handlerToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if err := skipJSONValue(decoder, handlerToken, arrayDepth+1); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, ']')
}

func skipJSONValue(decoder *json.Decoder, firstToken json.Token, depth int) error {
	if err := validateDepth(depth); err != nil {
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
		if err := validateDepth(depth + 1); err != nil {
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
		if err := skipJSONValue(decoder, valueToken, depth+1); err != nil {
			return err
		}
	}
	return consumeClosing(decoder, closing)
}

func validateDepth(depth int) error {
	if depth <= MaximumDepth {
		return nil
	}
	return fmt.Errorf(
		"%w: maximum=%d",
		jsonstrict.ErrMaximumDepthExceeded,
		MaximumDepth,
	)
}

func consumeClosing(decoder *json.Decoder, expected json.Delim) error {
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != expected {
		return fmt.Errorf("unexpected JSON delimiter %v, want %q", closing, expected)
	}
	return nil
}
