//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"example.com/event-registration-api/internal/core"
	"example.com/event-registration-api/internal/httpapi"
	"example.com/event-registration-api/internal/migrations"
	"example.com/event-registration-api/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"golang.org/x/crypto/bcrypt"
)

type testApp struct {
	db     *sql.DB
	server *httptest.Server
	store  *store.Postgres
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatal(err)
	}
	repo := store.NewPostgres(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer := httptest.NewServer(httpapi.NewServer(core.NewService(repo), repo, logger).Handler())
	t.Cleanup(httpServer.Close)
	return &testApp{db: db, server: httpServer, store: repo}
}

func (a *testApp) request(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, a.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func expectStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, body)
	}
}

func (a *testApp) member(t *testing.T, suffix string) string {
	t.Helper()
	email := fmt.Sprintf("member-%s-%d@example.com", suffix, time.Now().UnixNano())
	password := "a-long-test-password"
	status, body := a.request(t, http.MethodPost, "/v1/auth/register", "", map[string]any{
		"name": "Test Member", "email": email, "password": password,
	})
	expectStatus(t, status, http.StatusCreated, body)
	status, body = a.request(t, http.MethodPost, "/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	expectStatus(t, status, http.StatusOK, body)
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Token == "" {
		t.Fatalf("invalid login response: %s (%v)", body, err)
	}
	return response.Token
}

func (a *testApp) admin(t *testing.T) string {
	t.Helper()
	email := fmt.Sprintf("admin-%d@example.com", time.Now().UnixNano())
	password := "a-long-admin-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.UpsertAdmin(context.Background(), "Test Admin", email, string(hash)); err != nil {
		t.Fatal(err)
	}
	status, body := a.request(t, http.MethodPost, "/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	expectStatus(t, status, http.StatusOK, body)
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Token == "" {
		t.Fatalf("invalid admin login response: %s (%v)", body, err)
	}
	return response.Token
}

func (a *testApp) event(t *testing.T, token string, capacity int) string {
	t.Helper()
	status, body := a.request(t, http.MethodPost, "/v1/events", token, map[string]any{
		"title": "Go community night", "description": "Hands-on workshop",
		"startsAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), "capacity": capacity,
	})
	expectStatus(t, status, http.StatusCreated, body)
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.ID == "" {
		t.Fatalf("invalid event response: %s (%v)", body, err)
	}
	return response.ID
}

func TestRegistrationLifecycle(t *testing.T) {
	a := newTestApp(t)
	admin := a.admin(t)
	memberA := a.member(t, "a")
	memberB := a.member(t, "b")
	status, body := a.request(t, http.MethodPost, "/v1/events", memberA, map[string]any{
		"title": "Denied", "startsAt": time.Now().Add(time.Hour).Format(time.RFC3339), "capacity": 1,
	})
	expectStatus(t, status, http.StatusForbidden, body)
	eventID := a.event(t, admin, 1)
	path := "/v1/events/" + eventID
	status, body = a.request(t, http.MethodPost, path+"/registrations", memberA, nil)
	expectStatus(t, status, http.StatusCreated, body)
	status, body = a.request(t, http.MethodPost, path+"/registrations", memberA, nil)
	expectStatus(t, status, http.StatusConflict, body)
	status, body = a.request(t, http.MethodPost, path+"/registrations", memberB, nil)
	expectStatus(t, status, http.StatusConflict, body)
	if !bytes.Contains(body, []byte("EVENT_FULL")) {
		t.Fatalf("expected EVENT_FULL, got %s", body)
	}
	status, body = a.request(t, http.MethodDelete, path+"/registrations/me", memberA, nil)
	expectStatus(t, status, http.StatusNoContent, body)
	status, body = a.request(t, http.MethodPost, path+"/registrations", memberB, nil)
	expectStatus(t, status, http.StatusCreated, body)
	status, body = a.request(t, http.MethodGet, path+"/registrations", admin, nil)
	expectStatus(t, status, http.StatusOK, body)
	var listed struct {
		Items []core.EventRegistration `json:"items"`
	}
	if err := json.Unmarshal(body, &listed); err != nil || len(listed.Items) != 2 {
		t.Fatalf("expected two registration records, got %s (%v)", body, err)
	}
	status, body = a.request(t, http.MethodPost, "/v1/auth/logout", memberA, nil)
	expectStatus(t, status, http.StatusNoContent, body)
	status, body = a.request(t, http.MethodGet, "/v1/me", memberA, nil)
	expectStatus(t, status, http.StatusUnauthorized, body)
	assertSeats(t, a.db, eventID, 0, 1)
}

func TestLastSeatIsAtomic(t *testing.T) {
	a := newTestApp(t)
	eventID := a.event(t, a.admin(t), 1)
	tokens := []string{a.member(t, "concurrent-a"), a.member(t, "concurrent-b")}
	statuses := make(chan int, len(tokens))
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			status, _ := a.request(t, http.MethodPost, "/v1/events/"+eventID+"/registrations", token, nil)
			statuses <- status
		}(token)
	}
	wg.Wait()
	close(statuses)
	created, conflict := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected concurrent response status %d", status)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("created=%d, conflict=%d; want 1 each", created, conflict)
	}
	assertSeats(t, a.db, eventID, 0, 1)
}

func assertSeats(t *testing.T, db *sql.DB, eventID string, wantRemaining, wantActive int) {
	t.Helper()
	var remaining, active int
	if err := db.QueryRow(`SELECT remaining FROM events WHERE id = $1::uuid`, eventID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM registrations WHERE event_id = $1::uuid AND cancelled_at IS NULL`, eventID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if remaining != wantRemaining || active != wantActive {
		t.Fatalf("remaining=%d, active=%d; want %d, %d", remaining, active, wantRemaining, wantActive)
	}
}
