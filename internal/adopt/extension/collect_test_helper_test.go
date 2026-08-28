package extension

import "context"

func collectForTest(input Input) (Result, error) {
	return Collect(context.Background(), input, func(Skip) error { return nil })
}
