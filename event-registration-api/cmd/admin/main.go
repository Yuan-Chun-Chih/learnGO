package main

import (
	"context"
	"database/sql"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/event-registration-api/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	email := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL")))
	password := os.Getenv("ADMIN_PASSWORD")
	name := strings.TrimSpace(os.Getenv("ADMIN_NAME"))
	if name == "" {
		name = "Administrator"
	}
	address, emailErr := mail.ParseAddress(email)
	if dsn == "" || emailErr != nil || address.Address != email || len(email) > 254 ||
		utf8.RuneCountInString(name) > 80 || len(password) < 12 || len(password) > 72 {
		log.Fatal("valid DATABASE_URL, ADMIN_EMAIL, ADMIN_NAME and a 12-72 byte ADMIN_PASSWORD are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	user, err := store.NewPostgres(db).UpsertAdmin(ctx, name, email, string(hash))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("admin ready: %s (%s)", user.Email, user.ID)
}
