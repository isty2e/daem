package journal

import journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

// RootedCapabilityResolver acquires a borrowed, current capability for one
// already-bound global destination. A non-nil budget must cover acquisition
// before descriptor validation begins. The caller retains the root authority;
// the receiver closes each returned capability.
type RootedCapabilityResolver = journalnamespace.CapabilityResolver
