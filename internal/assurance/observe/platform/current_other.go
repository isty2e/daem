//go:build !darwin

package platform

func currentCommandRunner() (commandRunner, bool) {
	return nil, false
}
