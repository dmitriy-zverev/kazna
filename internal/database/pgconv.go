package database

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrNullValue is returned when a NULL appears where a value is required.
var ErrNullValue = errors.New("database: unexpected NULL")

// ToUUID converts a domain uuid into the pgx representation.
func ToUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(u), Valid: true}
}

// FromUUID converts a pgx uuid into the domain representation.
func FromUUID(p pgtype.UUID) (uuid.UUID, error) {
	if !p.Valid {
		return uuid.Nil, ErrNullValue
	}
	return uuid.UUID(p.Bytes), nil
}

// ToTimestamp converts a time into a non-null pgx timestamp.
func ToTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// ToTimestampPtr converts an optional time into a possibly-null pgx timestamp.
func ToTimestampPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// FromTimestamp converts a non-null pgx timestamp into a time.
func FromTimestamp(p pgtype.Timestamptz) (time.Time, error) {
	if !p.Valid {
		return time.Time{}, ErrNullValue
	}
	return p.Time, nil
}

// TimestampPtr converts a possibly-null pgx timestamp into an optional time.
func TimestampPtr(p pgtype.Timestamptz) *time.Time {
	if !p.Valid {
		return nil
	}
	t := p.Time
	return &t
}
