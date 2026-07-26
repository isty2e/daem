package access

import (
	"fmt"
	"sync"
)

// Materialization owns temporary artifact staging and its cleanup exactly once.
// It is a single-owner capability; only Release is safe to repeat.
type Materialization struct {
	mutex       sync.Mutex
	releaseOnce sync.Once
	view        View
	release     func() error
	released    bool
	releaseErr  error
}

// NewMaterialization takes ownership of temporary staging represented by view.
func NewMaterialization(view View, release func() error) (*Materialization, error) {
	if err := view.validate(); err != nil {
		return nil, err
	}
	if release == nil {
		return nil, fmt.Errorf("artifact materialization release is required")
	}
	return &Materialization{view: view, release: release}, nil
}

// View returns the non-owning view while the materialization is live.
func (materialization *Materialization) View() (View, error) {
	if materialization == nil {
		return View{}, fmt.Errorf("artifact materialization is required")
	}
	materialization.mutex.Lock()
	defer materialization.mutex.Unlock()
	if materialization.released {
		return View{}, fmt.Errorf("artifact materialization has been released")
	}
	return materialization.view, nil
}

// Release cleans owned staging once and returns the same cleanup result on
// every later call.
func (materialization *Materialization) Release() error {
	if materialization == nil {
		return fmt.Errorf("artifact materialization is required")
	}
	materialization.releaseOnce.Do(func() {
		materialization.mutex.Lock()
		materialization.released = true
		release := materialization.release
		materialization.release = nil
		materialization.mutex.Unlock()

		materialization.releaseErr = release()
	})
	return materialization.releaseErr
}
