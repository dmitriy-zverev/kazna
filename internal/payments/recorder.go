package payments

import (
	"time"

	"github.com/dmitriy-zverev/kazna/internal/providers"
)

// Recorder receives observability signals. Implementations must be safe for
// concurrent use. A nil Recorder is ignored.
type Recorder interface {
	// PaymentCreation counts a create request by outcome:
	// created, creation_unknown, replayed or rejected.
	PaymentCreation(result string)
	// ProviderCall records one provider interaction. code is empty on success.
	ProviderCall(operation string, code providers.ErrorCode, duration time.Duration)
}

// Creation outcomes reported to the Recorder.
const (
	OutcomeCreated         = "created"
	OutcomeCreationUnknown = "creation_unknown"
	OutcomeReplayed        = "replayed"
	OutcomeRejected        = "rejected"
)

func (s *Service) recordCreation(result string) {
	if s.recorder != nil {
		s.recorder.PaymentCreation(result)
	}
}

func (s *Service) recordProviderCall(operation string, err error, started time.Time) {
	if s.recorder == nil {
		return
	}
	code := providers.ErrorCode("")
	if err != nil {
		code = providers.CodeOf(err)
	}
	s.recorder.ProviderCall(operation, code, time.Since(started))
}
