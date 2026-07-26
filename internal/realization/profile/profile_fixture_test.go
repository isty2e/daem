package profile

func discoveryPaths(locations []DiscoveryLocation) []string {
	result := make([]string, 0, len(locations))
	for _, location := range locations {
		result = append(result, location.Path())
	}
	return result
}
