package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"example.com/event-registration-api/internal/core"
)

type pinger interface{ Ping(context.Context) error }

type Server struct {
	service *core.Service
	db      pinger
	logger  *slog.Logger
	handler http.Handler
}

func NewServer(service *core.Service, db pinger, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{service: service, db: db, logger: logger}
	s.handler = s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/auth/register", s.registerUser)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("POST /v1/auth/logout", s.logout)
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("GET /v1/me/registrations", s.listMyRegistrations)
	mux.HandleFunc("POST /v1/events", s.createEvent)
	mux.HandleFunc("GET /v1/events", s.listEvents)
	mux.HandleFunc("GET /v1/events/{id}", s.getEvent)
	mux.HandleFunc("PATCH /v1/events/{id}", s.updateEvent)
	mux.HandleFunc("POST /v1/events/{id}/close", s.closeEvent)
	mux.HandleFunc("POST /v1/events/{id}/registrations", s.registerForEvent)
	mux.HandleFunc("DELETE /v1/events/{id}/registrations/me", s.cancelRegistration)
	mux.HandleFunc("GET /v1/events/{id}/registrations", s.listEventRegistrations)
	var handler http.Handler = mux
	handler = s.recoverPanic(handler)
	handler = s.logging(handler)
	handler = requestID(handler)
	return handler
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		s.logger.Warn("database readiness failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type registerUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) registerUser(w http.ResponseWriter, r *http.Request) {
	var input registerUserRequest
	if !decodeOrReply(w, r, &input) {
		return
	}
	user, err := s.service.RegisterUser(r.Context(), input.Name, input.Email, input.Password)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if !decodeOrReply(w, r, &input) {
		return
	}
	token, user, err := s.service.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Token string    `json:"token"`
		User  core.User `json:"user"`
	}{Token: token, User: user})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, token, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	if err := s.service.Logout(r.Context(), token); err != nil {
		s.serviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.currentUser(w, r)
	if ok {
		writeJSON(w, http.StatusOK, user)
	}
}

type createEventRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartsAt    time.Time `json:"startsAt"`
	Capacity    int       `json:"capacity"`
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var input createEventRequest
	if !decodeOrReply(w, r, &input) {
		return
	}
	event, err := s.service.CreateEvent(r.Context(), actor, core.CreateEventInput{
		Title: input.Title, Description: input.Description,
		StartsAt: input.StartsAt, Capacity: input.Capacity,
	})
	if err != nil {
		s.serviceError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/events/"+event.ID)
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	event, err := s.service.GetEvent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePage(r)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	events, err := s.service.ListEvents(r.Context(), limit, offset)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events, "limit": limit, "offset": offset})
}

type updateEventRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func (s *Server) updateEvent(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var input updateEventRequest
	if !decodeOrReply(w, r, &input) {
		return
	}
	event, err := s.service.UpdateEvent(r.Context(), actor, r.PathValue("id"), core.UpdateEventInput{
		Title: input.Title, Description: input.Description,
	})
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) closeEvent(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	event, err := s.service.CloseEvent(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) registerForEvent(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	registration, err := s.service.Register(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, registration)
}

func (s *Server) cancelRegistration(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	if err := s.service.CancelRegistration(r.Context(), actor, r.PathValue("id")); err != nil {
		s.serviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMyRegistrations(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	limit, offset, err := parsePage(r)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	items, err := s.service.ListMyRegistrations(r.Context(), actor, limit, offset)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) listEventRegistrations(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	limit, offset, err := parsePage(r)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	items, err := s.service.ListEventRegistrations(r.Context(), actor, r.PathValue("id"), limit, offset)
	if err != nil {
		s.serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func parsePage(r *http.Request) (int, int, error) {
	limit, offset := 20, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: limit must be an integer", core.ErrInvalid)
		}
		limit = value
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: offset must be an integer", core.ErrInvalid)
		}
		offset = value
	}
	return limit, offset, nil
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (core.User, string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Bearer token required")
		return core.User{}, "", false
	}
	token = strings.TrimSpace(token)
	user, err := s.service.Authenticate(r.Context(), token)
	if err != nil {
		s.serviceError(w, err)
		return core.User{}, "", false
	}
	return user, token, true
}

func (s *Server) serviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, core.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid credentials or session")
	case errors.Is(err, core.ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "permission denied")
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, core.ErrAlreadyRegistered):
		writeError(w, http.StatusConflict, "ALREADY_REGISTERED", "already registered for this event")
	case errors.Is(err, core.ErrFull):
		writeError(w, http.StatusConflict, "EVENT_FULL", "event is full")
	case errors.Is(err, core.ErrClosed):
		writeError(w, http.StatusConflict, "EVENT_CLOSED", "event is closed or has started")
	case errors.Is(err, core.ErrConflict):
		writeError(w, http.StatusConflict, "CONFLICT", "resource already exists")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusRequestTimeout, "REQUEST_TIMEOUT", "request was cancelled or timed out")
	default:
		s.logger.Error("unhandled service error", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

func decodeOrReply(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(append(encoded, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}

type requestIDKey struct{}

var requestCounter atomic.Uint64

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("req-%d", requestCounter.Add(1))
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status, w.wrote = status, true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorded := &statusWriter{ResponseWriter: w}
		defer func() {
			status := recorded.status
			if status == 0 {
				status = http.StatusOK
			}
			s.logger.Info("http request", "request_id", r.Context().Value(requestIDKey{}),
				"method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
		}()
		next.ServeHTTP(recorded, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := &statusWriter{ResponseWriter: w}
		defer func() {
			if value := recover(); value != nil {
				s.logger.Error("panic in request", "error", value)
				if !recorded.wrote {
					writeError(recorded, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
				}
			}
		}()
		next.ServeHTTP(recorded, r)
	})
}
