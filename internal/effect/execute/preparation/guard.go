package preparation

import "errors"

// ErrNativeOperationRequired marks work the ordinary Apply pipeline cannot execute.
var ErrNativeOperationRequired = errors.New("native document work requires its containing execution pipeline")
