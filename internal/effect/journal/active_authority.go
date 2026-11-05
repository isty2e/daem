package journal

import journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

// ActiveJournalAuthority is the physical-directory evidence carried by the
// capture and recovery APIs. Inventory owns its admission and comparisons.
type ActiveJournalAuthority = journalinventory.ActiveJournalAuthority
