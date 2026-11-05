package recovery

import "fmt"

func (state RemovalState) Kind() (string, bool, error) {
	if before, present := state.Before(); present {
		return before.Kind, before.Existed, nil
	}
	if expected, present := state.Expected(); present {
		return expected.Kind, expected.Existed, nil
	}
	return "", false, fmt.Errorf("removal state is empty")
}
