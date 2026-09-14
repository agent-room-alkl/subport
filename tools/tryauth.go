//go:build ignore

package main

import (
	"fmt"
	"github.com/agent-room-alkl/subport/internal/store"
	"os"
)

func main() {
	fmt.Println("DATABASE_URL set", os.Getenv("DATABASE_URL") != "")
	st, err := store.OpenStore("subport-data.json", "http://127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	defer st.Close()
	fmt.Println("driver", st.Driver())
	u, err := st.Authenticate("admin", "subport-admin")
	fmt.Printf("auth err=%v user=%+v\n", err, u)
	users := []any{}
	// list via raw if needed
	_ = users
}
