package fileset

import "fmt"

// RecoveryAuthorityError identifies a recorded entry outside current authority.
type RecoveryAuthorityError struct {
	Path string
}

func (err RecoveryAuthorityError) Error() string {
	return fmt.Sprintf("interrupted file-set transaction target %q is outside current recovery authority", err.Path)
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
