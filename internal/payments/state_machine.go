package payments

import "fmt"

// allowedTransitions is the only place that decides which payment status
// changes are legal. External provider statuses never drive transitions
// directly; they are mapped to an internal status and validated here.
//
//	created ──▶ creating ──┬──▶ pending ──┬──▶ succeeded
//	                       │              └──▶ canceled
//	                       └──▶ creation_unknown ──┬──▶ pending
//	                                                ├──▶ succeeded
//	                                                └──▶ canceled
//
// succeeded and canceled are terminal: a late webhook must never move a
// succeeded payment back to pending.
var allowedTransitions = map[Status]map[Status]bool{
	StatusCreated: {
		StatusCreating: true,
		StatusCanceled: true,
	},
	StatusCreating: {
		StatusPending:         true,
		StatusCreationUnknown: true,
		StatusCanceled:        true,
	},
	StatusCreationUnknown: {
		StatusPending:   true,
		StatusSucceeded: true,
		StatusCanceled:  true,
	},
	StatusPending: {
		StatusSucceeded: true,
		StatusCanceled:  true,
	},
	StatusSucceeded: {},
	StatusCanceled:  {},
}

// CanTransition reports whether from → to is a legal status change.
func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}

// ValidateTransition returns an error describing an illegal status change.
func ValidateTransition(from, to Status) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
}
