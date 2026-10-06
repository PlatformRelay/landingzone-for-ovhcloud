package live

import "time"

// DefaultDeadline bounds a live run when no --deadline is given (FR-011: default 45 min, then
// the destroy-on-exit fires).
//
// STUB (T054): T055 sets it.
const DefaultDeadline time.Duration = 0
