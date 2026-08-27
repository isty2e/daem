//go:build !linux && !darwin

package transaction

import "context"

func observeStateDirPlatform(context.Context, string) (string, string, error) {
	return "", "", nil
}
