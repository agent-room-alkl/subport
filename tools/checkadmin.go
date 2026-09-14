//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"github.com/agent-room-alkl/subport/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
)

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	var id, user, hash, salt, role string
	err = db.QueryRow(`SELECT id,username,password_hash,password_salt,role FROM users WHERE username=$1`, "admin").Scan(&id, &user, &hash, &salt, &role)
	if err != nil {
		panic(err)
	}
	fmt.Printf("id=%s user=%s role=%s salt=%q hash=%s\n", id, user, role, salt, hash)
	want := store.HashWithSalt("subport-admin", salt)
	fmt.Printf("want_with_existing_salt=%s match=%v\n", want, want == hash)
	// Also show length
	fmt.Printf("hash_len=%d salt_len=%d\n", len(hash), len(salt))
}
