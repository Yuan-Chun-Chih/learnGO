package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalid           = errors.New("invalid input")
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrFull              = errors.New("event full")
	ErrClosed            = errors.New("event closed")
	ErrAlreadyRegistered = errors.New("already registered")
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type Account struct {
	User
	PasswordHash string
}

type Event struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartsAt    time.Time `json:"startsAt"`
	Capacity    int       `json:"capacity"`
	Remaining   int       `json:"remaining"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Registration struct {
	ID          string     `json:"id"`
	EventID     string     `json:"eventId"`
	UserID      string     `json:"userId"`
	CreatedAt   time.Time  `json:"createdAt"`
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
}

type MyRegistration struct {
	Registration
	EventTitle string    `json:"eventTitle"`
	StartsAt   time.Time `json:"startsAt"`
}

type EventRegistration struct {
	Registration
	UserName  string `json:"userName"`
	UserEmail string `json:"userEmail"`
}

type CreateEventInput struct {
	Title       string
	Description string
	StartsAt    time.Time
	Capacity    int
}

type UpdateEventInput struct {
	Title       *string
	Description *string
}

// Store is defined by the business layer. PostgreSQL is one implementation.
type Store interface {
	CreateUser(context.Context, string, string, string) (User, error)
	FindAccountByEmail(context.Context, string) (Account, error)
	CreateSession(context.Context, string, string, time.Time) error
	FindSessionUser(context.Context, string) (User, error)
	RevokeSession(context.Context, string) error
	CreateEvent(context.Context, CreateEventInput) (Event, error)
	GetEvent(context.Context, string) (Event, error)
	ListEvents(context.Context, int, int) ([]Event, error)
	UpdateEvent(context.Context, string, UpdateEventInput) (Event, error)
	CloseEvent(context.Context, string) (Event, error)
	Register(context.Context, string, string) (Registration, error)
	CancelRegistration(context.Context, string, string) error
	ListMyRegistrations(context.Context, string, int, int) ([]MyRegistration, error)
	ListEventRegistrations(context.Context, string, int, int) ([]EventRegistration, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) RegisterUser(ctx context.Context, name, email, password string) (User, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
		return User{}, fmt.Errorf("%w: name must contain 1–80 characters", ErrInvalid)
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return User{}, fmt.Errorf("%w: email is invalid", ErrInvalid)
	}
	if len(password) < 12 || len(password) > 72 {
		return User{}, fmt.Errorf("%w: password must contain 12–72 bytes", ErrInvalid)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	return s.store.CreateUser(ctx, name, email, string(hash))
}

func (s *Service) Login(ctx context.Context, email, password string) (string, User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	account, err := s.store.FindAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", User{}, ErrUnauthorized
		}
		return "", User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return "", User{}, ErrUnauthorized
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", User{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.store.CreateSession(ctx, TokenHash(token), account.ID, s.now().Add(24*time.Hour)); err != nil {
		return "", User{}, err
	}
	return token, account.User, nil
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthorized
	}
	user, err := s.store.FindSessionUser(ctx, TokenHash(token))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrUnauthorized
	}
	return user, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrUnauthorized
	}
	return s.store.RevokeSession(ctx, TokenHash(token))
}

func (s *Service) CreateEvent(ctx context.Context, actor User, input CreateEventInput) (Event, error) {
	if actor.Role != "admin" {
		return Event{}, ErrForbidden
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 120 ||
		utf8.RuneCountInString(input.Description) > 2000 || input.Capacity < 1 || input.Capacity > 10000 ||
		!input.StartsAt.After(s.now()) {
		return Event{}, fmt.Errorf("%w: title, description, capacity or start time is invalid", ErrInvalid)
	}
	return s.store.CreateEvent(ctx, input)
}

func (s *Service) GetEvent(ctx context.Context, id string) (Event, error) {
	return s.store.GetEvent(ctx, id)
}

func (s *Service) ListEvents(ctx context.Context, limit, offset int) ([]Event, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return nil, fmt.Errorf("%w: pagination is out of range", ErrInvalid)
	}
	return s.store.ListEvents(ctx, limit, offset)
}

func (s *Service) UpdateEvent(ctx context.Context, actor User, id string, input UpdateEventInput) (Event, error) {
	if actor.Role != "admin" {
		return Event{}, ErrForbidden
	}
	if input.Title == nil && input.Description == nil {
		return Event{}, fmt.Errorf("%w: provide a title or description", ErrInvalid)
	}
	if input.Title != nil {
		trimmed := strings.TrimSpace(*input.Title)
		if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 120 {
			return Event{}, fmt.Errorf("%w: title must contain 1–120 characters", ErrInvalid)
		}
		input.Title = &trimmed
	}
	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(trimmed) > 2000 {
			return Event{}, fmt.Errorf("%w: description is too long", ErrInvalid)
		}
		input.Description = &trimmed
	}
	return s.store.UpdateEvent(ctx, id, input)
}

func (s *Service) CloseEvent(ctx context.Context, actor User, id string) (Event, error) {
	if actor.Role != "admin" {
		return Event{}, ErrForbidden
	}
	return s.store.CloseEvent(ctx, id)
}

func (s *Service) Register(ctx context.Context, actor User, eventID string) (Registration, error) {
	return s.store.Register(ctx, eventID, actor.ID)
}

func (s *Service) CancelRegistration(ctx context.Context, actor User, eventID string) error {
	return s.store.CancelRegistration(ctx, eventID, actor.ID)
}

func (s *Service) ListMyRegistrations(ctx context.Context, actor User, limit, offset int) ([]MyRegistration, error) {
	if err := validatePage(limit, offset); err != nil {
		return nil, err
	}
	return s.store.ListMyRegistrations(ctx, actor.ID, limit, offset)
}

func (s *Service) ListEventRegistrations(ctx context.Context, actor User, eventID string, limit, offset int) ([]EventRegistration, error) {
	if actor.Role != "admin" {
		return nil, ErrForbidden
	}
	if err := validatePage(limit, offset); err != nil {
		return nil, err
	}
	return s.store.ListEventRegistrations(ctx, eventID, limit, offset)
}

func validatePage(limit, offset int) error {
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return fmt.Errorf("%w: pagination is out of range", ErrInvalid)
	}
	return nil
}
