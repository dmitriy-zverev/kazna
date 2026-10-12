package payments

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestLegalTransitions(t *testing.T) {
	legal := []struct{ from, to Status }{
		{StatusCreated, StatusCreating},
		{StatusCreated, StatusCanceled},
		{StatusCreating, StatusPending},
		{StatusCreating, StatusCreationUnknown},
		{StatusCreating, StatusCanceled},
		{StatusCreationUnknown, StatusPending},
		{StatusCreationUnknown, StatusSucceeded},
		{StatusCreationUnknown, StatusCanceled},
		{StatusPending, StatusSucceeded},
		{StatusPending, StatusCanceled},
	}

	for _, tc := range legal {
		if err := ValidateTransition(tc.from, tc.to); err != nil {
			t.Fatalf("%s → %s must be legal: %v", tc.from, tc.to, err)
		}
	}
}

func TestIllegalTransitions(t *testing.T) {
	illegal := []struct{ from, to Status }{
		// A late notification must never roll a payment back.
		{StatusSucceeded, StatusPending},
		{StatusSucceeded, StatusCanceled},
		{StatusSucceeded, StatusCreationUnknown},
		{StatusCanceled, StatusPending},
		{StatusCanceled, StatusSucceeded},
		// Skipping the state machine.
		{StatusCreated, StatusSucceeded},
		{StatusCreated, StatusPending},
		{StatusPending, StatusCreating},
		{StatusPending, StatusCreationUnknown},
	}

	for _, tc := range illegal {
		if err := ValidateTransition(tc.from, tc.to); !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("%s → %s must be illegal, got %v", tc.from, tc.to, err)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []Status{StatusSucceeded, StatusCanceled} {
		if !s.Terminal() {
			t.Fatalf("%s must be terminal", s)
		}
		for _, other := range []Status{StatusCreated, StatusCreating, StatusCreationUnknown, StatusPending} {
			if CanTransition(s, other) {
				t.Fatalf("%s → %s must not be allowed", s, other)
			}
		}
	}
	for _, s := range []Status{StatusCreated, StatusCreating, StatusCreationUnknown, StatusPending} {
		if s.Terminal() {
			t.Fatalf("%s must not be terminal", s)
		}
	}
}

func TestEveryStatusIsReachable(t *testing.T) {
	// Every status must appear in the table, so a new state cannot be added
	// without deciding its transitions.
	for _, s := range []Status{
		StatusCreated, StatusCreating, StatusCreationUnknown,
		StatusPending, StatusSucceeded, StatusCanceled,
	} {
		if _, ok := allowedTransitions[s]; !ok {
			t.Fatalf("status %s is missing from the transition table", s)
		}
	}
	if len(allowedTransitions) != 6 {
		t.Fatalf("transition table has %d states, want 6", len(allowedTransitions))
	}
}

func TestRequestHashIsDeterministicAndSensitive(t *testing.T) {
	base := CreatePaymentInput{
		OrderID:     "order_1",
		AmountMinor: 49900,
		Currency:    "RUB",
		Description: "Sub",
		Metadata:    map[string]string{"b": "2", "a": "1"},
	}

	first, err := requestHash(base)
	if err != nil {
		t.Fatalf("requestHash: %v", err)
	}
	// Map iteration order must not change the hash.
	reordered := base
	reordered.Metadata = map[string]string{"a": "1", "b": "2"}
	second, err := requestHash(reordered)
	if err != nil {
		t.Fatalf("requestHash: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("hash depends on map iteration order")
	}

	changed := base
	changed.AmountMinor = 49901
	third, _ := requestHash(changed)
	if string(first) == string(third) {
		t.Fatal("hash ignores the amount")
	}
}

func TestMetadataIsSerializedAsObject(t *testing.T) {
	encoded, err := marshalMetadata(map[string]string{"user_id": "u1"})
	if err != nil {
		t.Fatalf("marshalMetadata: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["user_id"] != "u1" {
		t.Fatalf("decoded = %v", decoded)
	}
}
