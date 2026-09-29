package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"example.com/event-registration-api/internal/core"
	"github.com/jackc/pgx/v5/pgconn"
)

type Postgres struct {
	db *sql.DB
}

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

func (p *Postgres) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func (p *Postgres) CreateUser(ctx context.Context, name, email, passwordHash string) (core.User, error) {
	var u core.User
	err := p.db.QueryRowContext(ctx, `
		INSERT INTO users (name, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, email, role, created_at`, name, email, passwordHash,
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	return u, translate(err)
}

func (p *Postgres) FindAccountByEmail(ctx context.Context, email string) (core.Account, error) {
	var a core.Account
	err := p.db.QueryRowContext(ctx, `
		SELECT id::text, name, email, role, created_at, password_hash
		FROM users WHERE email = $1`, email,
	).Scan(&a.ID, &a.Name, &a.Email, &a.Role, &a.CreatedAt, &a.PasswordHash)
	return a, translate(err)
}

func (p *Postgres) CreateSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2::uuid, $3)`, tokenHash, userID, expiresAt)
	return translate(err)
}

func (p *Postgres) FindSessionUser(ctx context.Context, tokenHash string) (core.User, error) {
	var u core.User
	err := p.db.QueryRowContext(ctx, `
		SELECT u.id::text, u.name, u.email, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`, tokenHash,
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	return u, translate(err)
}

func (p *Postgres) RevokeSession(ctx context.Context, tokenHash string) error {
	result, err := p.db.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`, tokenHash)
	if err != nil {
		return translate(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrUnauthorized
	}
	return nil
}

func scanEvent(row interface{ Scan(...any) error }) (core.Event, error) {
	var e core.Event
	err := row.Scan(&e.ID, &e.Title, &e.Description, &e.StartsAt, &e.Capacity, &e.Remaining, &e.Status, &e.CreatedAt)
	return e, translate(err)
}

const eventColumns = `id::text, title, description, starts_at, capacity, remaining, status, created_at`

func (p *Postgres) CreateEvent(ctx context.Context, input core.CreateEventInput) (core.Event, error) {
	return scanEvent(p.db.QueryRowContext(ctx, `
		INSERT INTO events (title, description, starts_at, capacity, remaining)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING `+eventColumns, input.Title, input.Description, input.StartsAt, input.Capacity))
}

func (p *Postgres) GetEvent(ctx context.Context, id string) (core.Event, error) {
	return scanEvent(p.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = $1::uuid`, id))
}

func (p *Postgres) ListEvents(ctx context.Context, limit, offset int) ([]core.Event, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT `+eventColumns+` FROM events
		ORDER BY starts_at, id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	out := make([]core.Event, 0)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateEvent(ctx context.Context, id string, input core.UpdateEventInput) (core.Event, error) {
	e, err := scanEvent(p.db.QueryRowContext(ctx, `
		UPDATE events SET title = COALESCE($2, title), description = COALESCE($3, description)
		WHERE id = $1::uuid AND status = 'open' AND starts_at > now()
		RETURNING `+eventColumns, id, input.Title, input.Description))
	if !errors.Is(err, core.ErrNotFound) {
		return e, err
	}
	if _, lookupErr := p.GetEvent(ctx, id); lookupErr != nil {
		return core.Event{}, lookupErr
	}
	return core.Event{}, core.ErrClosed
}

func (p *Postgres) CloseEvent(ctx context.Context, id string) (core.Event, error) {
	return scanEvent(p.db.QueryRowContext(ctx, `
		UPDATE events SET status = 'closed'
		WHERE id = $1::uuid RETURNING `+eventColumns, id))
}

func (p *Postgres) Register(ctx context.Context, eventID, userID string) (core.Registration, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Registration{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE events SET remaining = remaining - 1
		WHERE id = $1::uuid AND status = 'open' AND starts_at > now() AND remaining > 0`, eventID)
	if err != nil {
		return core.Registration{}, translate(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return core.Registration{}, err
	}
	if n == 0 {
		var status string
		var startsAt time.Time
		var remaining int
		err := tx.QueryRowContext(ctx, `SELECT status, starts_at, remaining FROM events WHERE id = $1::uuid`, eventID).
			Scan(&status, &startsAt, &remaining)
		if err != nil {
			return core.Registration{}, translate(err)
		}
		if status != "open" || !startsAt.After(time.Now()) {
			return core.Registration{}, core.ErrClosed
		}
		return core.Registration{}, core.ErrFull
	}

	var registration core.Registration
	err = tx.QueryRowContext(ctx, `
		INSERT INTO registrations (event_id, user_id)
		VALUES ($1::uuid, $2::uuid)
		RETURNING id::text, event_id::text, user_id::text, created_at`, eventID, userID,
	).Scan(&registration.ID, &registration.EventID, &registration.UserID, &registration.CreatedAt)
	if err != nil {
		if pgCode(err) == "23505" {
			return core.Registration{}, core.ErrAlreadyRegistered
		}
		return core.Registration{}, translate(err)
	}
	if err := tx.Commit(); err != nil {
		return core.Registration{}, err
	}
	return registration, nil
}

func (p *Postgres) CancelRegistration(ctx context.Context, eventID, userID string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, `
		UPDATE registrations SET cancelled_at = now()
		WHERE event_id = $1::uuid AND user_id = $2::uuid AND cancelled_at IS NULL
		RETURNING id::text`, eventID, userID).Scan(&id)
	if err != nil {
		return translate(err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE events SET remaining = remaining + 1
		WHERE id = $1::uuid AND remaining < capacity`, eventID)
	if err != nil {
		return translate(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("cancel registration: event capacity invariant violated")
	}
	return tx.Commit()
}

func (p *Postgres) ListMyRegistrations(ctx context.Context, userID string, limit, offset int) ([]core.MyRegistration, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT r.id::text, r.event_id::text, r.user_id::text, r.created_at,
		       r.cancelled_at, e.title, e.starts_at
		FROM registrations r JOIN events e ON e.id = r.event_id
		WHERE r.user_id = $1::uuid
		ORDER BY r.created_at DESC, r.id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	out := make([]core.MyRegistration, 0)
	for rows.Next() {
		var item core.MyRegistration
		var cancelled sql.NullTime
		if err := rows.Scan(&item.ID, &item.EventID, &item.UserID, &item.CreatedAt, &cancelled, &item.EventTitle, &item.StartsAt); err != nil {
			return nil, err
		}
		if cancelled.Valid {
			item.CancelledAt = &cancelled.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *Postgres) ListEventRegistrations(ctx context.Context, eventID string, limit, offset int) ([]core.EventRegistration, error) {
	if _, err := p.GetEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx, `
		SELECT r.id::text, r.event_id::text, r.user_id::text, r.created_at,
		       r.cancelled_at, u.name, u.email
		FROM registrations r JOIN users u ON u.id = r.user_id
		WHERE r.event_id = $1::uuid
		ORDER BY r.created_at, r.id LIMIT $2 OFFSET $3`, eventID, limit, offset)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()
	out := make([]core.EventRegistration, 0)
	for rows.Next() {
		var item core.EventRegistration
		var cancelled sql.NullTime
		if err := rows.Scan(&item.ID, &item.EventID, &item.UserID, &item.CreatedAt, &cancelled, &item.UserName, &item.UserEmail); err != nil {
			return nil, err
		}
		if cancelled.Valid {
			item.CancelledAt = &cancelled.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *Postgres) UpsertAdmin(ctx context.Context, name, email, passwordHash string) (core.User, error) {
	var u core.User
	err := p.db.QueryRowContext(ctx, `
		INSERT INTO users (name, email, password_hash, role)
		VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (email) DO UPDATE SET
		    name = EXCLUDED.name, password_hash = EXCLUDED.password_hash, role = 'admin'
		RETURNING id::text, name, email, role, created_at`, name, email, passwordHash,
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	return u, translate(err)
}

func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return core.ErrNotFound
	}
	switch pgCode(err) {
	case "22P02":
		return fmt.Errorf("%w: malformed ID", core.ErrInvalid)
	case "23505":
		return core.ErrConflict
	case "23503":
		return core.ErrNotFound
	case "23514", "23502":
		return core.ErrInvalid
	default:
		return err
	}
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
