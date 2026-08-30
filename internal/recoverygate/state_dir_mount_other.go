//go:build !linux && !darwin

package recoverygate

import "context"

func observeStateDirPlatform(context.Context, string) (string, string, error) {
	return "", "", nil
}
