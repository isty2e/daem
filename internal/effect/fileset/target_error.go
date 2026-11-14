package fileset

import "fmt"

// RecoveryAuthorityError identifies a recorded entry outside current authority.
type RecoveryAuthorityError struct {
	Path string
}

func (err RecoveryAuthorityError) Error() string {
	return fmt.Sprintf("interrupted file-set transaction target %q is outside current recovery authority", err.Path)
}

// RecoveryTargetCountError distinguishes a different finite transaction shape
// from a failure to read or validate its evidence.
type RecoveryTargetCountError struct {
	Expected int
	Recorded int
}

func (err RecoveryTargetCountError) Error() string {
	return "interrupted file-set target set does not match this operation"
}

// TargetWriteError preserves the exact entry whose publication failed.
type TargetWriteError struct {
	Path string
	Err  error
}

func (err TargetWriteError) Error() string {
	return fmt.Sprintf("write target %q: %v", err.Path, err.Err)
}

func (err TargetWriteError) Unwrap() error { return err.Err }
