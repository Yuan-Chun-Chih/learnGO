package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegisterUserRejectsInvalidInput(t *testing.T) {
	svc := NewService(nil)
	for _, tc := range []struct {
		name, email, password string
	}{
		{"", "a@example.com", "long-enough-password"},
		{"Alice", "not-an-email", "long-enough-password"},
		{"Alice", "a@example.com", "short"},
		{"Alice", "a@example.com", strings.Repeat("x", 73)},
	} {
		_, err := svc.RegisterUser(context.Background(), tc.name, tc.email, tc.password)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %+v: expected ErrInvalid, got %v", tc, err)
		}
	}
}

func TestEventValidationAndAuthorization(t *testing.T) {
	svc := NewService(nil)
	input := CreateEventInput{Title: "Go meetup", StartsAt: time.Now().Add(time.Hour), Capacity: 10}
	if _, err := svc.CreateEvent(context.Background(), User{Role: "member"}, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member create: expected ErrForbidden, got %v", err)
	}
	input.Capacity = 0
	if _, err := svc.CreateEvent(context.Background(), User{Role: "admin"}, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero capacity: expected ErrInvalid, got %v", err)
	}
	if _, err := svc.ListEvents(context.Background(), 101, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid page: expected ErrInvalid, got %v", err)
	}
}

func TestTokenHash(t *testing.T) {
	if TokenHash("secret") == "secret" || TokenHash("secret") == TokenHash("other") || len(TokenHash("secret")) != 64 {
		t.Fatal("token hash must be deterministic, distinct and SHA-256 hex")
	}
}
