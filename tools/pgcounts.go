//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
)

func main() {
	db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	defer db.Close()
	for _, t := range []string{"users", "accounts", "api_keys", "model_routes", "sessions", "usage_logs"} {
		var n int
		db.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n)
		fmt.Printf("%s=%d\n", t, n)
	}
}
