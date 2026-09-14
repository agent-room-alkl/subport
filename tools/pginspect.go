//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		panic(err)
	}
	rows, err := db.Query(`SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema='public' AND column_name IN ('enabled','healthy','stream_broken','compensated','created_at','expires_at','paid_at') ORDER BY table_name, column_name`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t, c, d string
		rows.Scan(&t, &c, &d)
		fmt.Printf("%s.%s = %s\n", t, c, d)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	fmt.Printf("users=%d\n", n)
	rows2, _ := db.Query(`SELECT username, role FROM users`)
	defer rows2.Close()
	for rows2.Next() {
		var u, r string
		rows2.Scan(&u, &r)
		fmt.Printf("user username=%s role=%s\n", u, r)
	}
}
