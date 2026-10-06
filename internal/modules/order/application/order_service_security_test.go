package application

import (
	"errors"
	"testing"
)

func TestValidateGuestPasswordRequiresEightCharactersForNewOrders(t *testing.T) {
	if err := validateGuestPassword("1234567"); !errors.Is(err, ErrGuestPasswordTooShort) {
		t.Fatalf("seven-character password error = %v", err)
	}
	if err := validateGuestPassword("12345678"); err != nil {
		t.Fatalf("eight-character password error = %v", err)
	}
}
