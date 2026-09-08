// Command subport starts the gateway. It does wiring only - every decision
// worth reading lives in internal/gateway (scheduling and failover),
// internal/store (persistence) or internal/httpapi (routes and access).
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/httpapi"
	"github.com/agent-room-alkl/subport/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := env("SUBPORT_ADDR", ":8080")

	// The demo upstream is served by this same process, so the seeded accounts
	// must point at wherever we are actually listening. Deriving it from addr
	// rather than hardcoding a port means changing SUBPORT_ADDR does not
	// silently break every upstream call.
	selfURL := env("SUBPORT_SELF_URL", "http://127.0.0.1"+addr)

	dbPath := env("SUBPORT_DB", "subport-data.json")
	st, err := store.OpenStore(dbPath, selfURL)
	if err != nil {
		log.Fatalf("cannot open store %s: %v", dbPath, err)
	}

	// Invite-gated unless explicitly set to empty, so a fresh deployment is
	// not open to mass registration by default.
	invite, set := os.LookupEnv("SUBPORT_INVITE_CODE")
	if !set {
		invite = "subport-invite"
	}

	adminPassword := env("SUBPORT_ADMIN_PASSWORD", "subport-admin")
	if _, err := st.Authenticate("admin", adminPassword); err != nil {
		if u, cerr := st.CreateUser("admin", adminPassword, "admin"); cerr == nil {
			log.Printf("created bootstrap admin %q - change this password before exposing the service", u.Username)
		}
	}

	sched := gateway.NewScheduler(st.Accounts())
	srv := httpapi.New(st, sched, invite, env("SUBPORT_WEB", "web"))

	log.Printf("subport listening on %s (store=%s, self=%s)", addr, dbPath, selfURL)
	log.Fatal(http.ListenAndServe(addr, srv))
}
