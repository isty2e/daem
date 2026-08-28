package extension

func collectForTest(input Input) (Result, error) {
	return Collect(input, func(Skip) error { return nil })
}
