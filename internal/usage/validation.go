package usage

import (
	"time"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func validate(event Event) error {
	if !bounded(event.ID) || !bounded(event.IdempotencyKey) || !bounded(event.TenantID) || !bounded(event.MembershipID) || !bounded(event.DeviceUID) || !bounded(event.Operation) || event.Units < 0 {
		return ErrInvalidEvent
	}
	if event.Outcome != "completed" && event.Outcome != "failed" && event.Outcome != "unknown" {
		return ErrInvalidEvent
	}
	if len(event.At) == 0 || len(event.At) > protocol.MaxUsageTimeBytes {
		return ErrInvalidEvent
	}
	stamp, err := time.Parse(time.RFC3339Nano, event.At)
	if err != nil || stamp.Year() < 1970 || stamp.Year() > 9999 || stamp.UTC().Format(time.RFC3339Nano) != event.At {
		return ErrInvalidEvent
	}
	return nil
}

func bounded(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value)
}
