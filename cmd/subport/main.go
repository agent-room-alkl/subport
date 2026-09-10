// Command subport starts the gateway. It does wiring only - every decision
// worth reading lives in internal/gateway (scheduling and failover),
// internal/store (persistence) or internal/httpapi (routes and access).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/httpapi"
	"github.com/agent-room-alkl/subport/internal/model"
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

	// Hydrate gateway credential cache from SQLite (DB -> runtime -> env/files).
	if creds, err := st.ListCredentialsForBootstrap(); err != nil {
		log.Printf("credential bootstrap warning: %v", err)
	} else {
		byID := map[string]model.Account{}
		for _, a := range st.Accounts() {
			byID[a.ID] = a
		}
		for _, c := range creds {
			gateway.SetAccountCredential(c.AccountID, c.AccessToken, c.RefreshToken, c.ExtraJSON, c.ExpiresAt)
			if a, ok := byID[c.AccountID]; ok {
				gateway.HydrateRuntimeFromAccount(c.AccountID, a.Provider)
			}
		}
	}

	// Claude.ai subscription bootstrap: exchange session/refresh env into an
	// in-memory OAuth access token, then ensure a healthy provider=claude
	// account exists and pause demo mock peers in tier 1.
	if err := gateway.EnsureClaudeTokensFromEnv(); err != nil {
		log.Printf("claude bootstrap warning: %v", err)
	}
	if gateway.HasClaudeCredential() {
		acct := model.Account{
			ID:       "acct-claude-1",
			Name:     "Claude.ai subscription",
			Provider: "claude",
			BaseURL:  "https://api.anthropic.com",
			Priority: 1,
			Healthy:  true,
		}
		if err := st.UpsertAccount(acct); err != nil {
			log.Printf("claude bootstrap: upsert account failed: %v", err)
		} else {
			log.Printf("claude bootstrap: account %s ready", acct.ID)
			syncBootstrapCredentials(st, acct.ID, "claude")
		}
	}

	// ChatGPT / Codex CLI subscription bootstrap: load ~/.codex/auth.json (or
	// token files) into an in-memory OAuth access token, then ensure a healthy
	// provider=codex account exists.
	if err := gateway.EnsureCodexTokensFromEnv(); err != nil {
		log.Printf("codex bootstrap warning: %v", err)
	}
	if gateway.HasCodexCredential() {
		acct := model.Account{
			ID:       "acct-codex-1",
			Name:     "ChatGPT Codex subscription",
			Provider: "codex",
			BaseURL:  "https://chatgpt.com",
			Priority: 1,
			Healthy:  true,
		}
		if err := st.UpsertAccount(acct); err != nil {
			log.Printf("codex bootstrap: upsert account failed: %v", err)
		} else {
			log.Printf("codex bootstrap: account %s ready", acct.ID)
			syncBootstrapCredentials(st, acct.ID, "codex")
		}
	}

	// Antigravity / Cloud Code subscription bootstrap.
	if err := gateway.EnsureAntigravityTokensFromEnv(); err != nil {
		log.Printf("antigravity bootstrap warning: %v", err)
	}
	{
		acct := model.Account{
			ID:       "acct-antigravity-1",
			Name:     "Antigravity subscription",
			Provider: "antigravity",
			BaseURL:  "https://cloudcode-pa.googleapis.com",
			Priority: 1,
			Healthy:  gateway.HasAntigravityCredential(),
		}
		if existing, err := st.AccountByID(acct.ID); err == nil {
			// Preserve operator health unless we just loaded live credentials.
			if gateway.HasAntigravityCredential() {
				acct.Healthy = true
			} else {
				acct.Healthy = existing.Healthy
			}
		}
		if err := st.UpsertAccount(acct); err != nil {
			log.Printf("antigravity bootstrap: upsert account failed: %v", err)
		} else {
			log.Printf("antigravity bootstrap: account %s ready (healthy=%v)", acct.ID, acct.Healthy)
			if gateway.HasAntigravityCredential() {
				syncBootstrapCredentials(st, acct.ID, "antigravity")
			}
		}
	}

	// Ensure gemini* models route to antigravity even on existing DBs.
	if err := st.UpsertModelRoute(model.ModelRoute{
		ID: "route-antigravity", Pattern: `^gemini|^tab_flash|^gpt-oss`, Provider: "antigravity", Priority: 1, Enabled: true,
	}); err != nil {
		log.Printf("antigravity bootstrap: model route warning: %v", err)
	}

	// Pause demo mock peers whenever any real subscription account is online.
	if gateway.HasClaudeCredential() || gateway.HasCodexCredential() || gateway.HasAntigravityCredential() {
		for _, a := range st.Accounts() {
			if a.Provider == "mock" && a.Healthy {
				if err := st.SetAccountHealthy(a.ID, false); err == nil {
					log.Printf("subscription bootstrap: paused mock account %s", a.ID)
				}
			}
		}
	}

	sched := gateway.NewScheduler(st.Accounts())
	sched.SetModelRoutes(st.ListModelRoutes())
	sched.SetHealthStore(st)
	gateway.HydrateProxies(st.ListProxies())

	stopBG := make(chan struct{})
	gateway.StartTokenRefreshLoop(stopBG, gateway.DefaultRefreshInterval, store.TokenRefreshBridge{Store: st})
	gateway.StartHealthReaper(stopBG, sched, time.Minute)

	// Compensation config: when a user's stream_broken rate over a window
	// crosses the threshold, the cost of broken calls is credited back.
	// Defaults are conservative: 30% broken rate over the last 10 calls,
	// with at least 2 broken calls to trigger (avoids 1-off noise).
	compCfg := model.CompensationConfig{
		Threshold:  0.3,
		WindowSize: 10,
		MinBroken:  2,
	}
	if v := os.Getenv("SUBPORT_COMP_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			compCfg.Threshold = f
		}
	}
	if v := os.Getenv("SUBPORT_COMP_WINDOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			compCfg.WindowSize = n
		}
	}
	if v := os.Getenv("SUBPORT_COMP_MIN_BROKEN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			compCfg.MinBroken = n
		}
	}

	api := httpapi.New(st, sched, invite, env("SUBPORT_WEB", "web/admin-vue/dist"), compCfg)
	httpSrv := &http.Server{Addr: addr, Handler: api}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("subport shutting down on %v", sig)
		close(stopBG)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("subport shutdown: %v", err)
		}
	}()

	log.Printf("subport listening on %s (store=%s, self=%s)", addr, dbPath, selfURL)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}


// syncBootstrapCredentials upserts runtime/env tokens into account_credentials
// and the gateway cache so admin UI flags stay in sync. Never logs token values.
func syncBootstrapCredentials(st *store.Store, accountID, provider string) {
	access, refresh, extra, expiresAt, ok := gateway.RuntimeCredentialMaterial(provider)
	if !ok {
		return
	}
	patch := store.CredentialPatch{}
	if access != "" {
		patch.AccessToken = &access
	}
	if refresh != "" {
		patch.RefreshToken = &refresh
	}
	if extra != "" {
		patch.ExtraJSON = &extra
	}
	if expiresAt != "" {
		patch.ExpiresAt = &expiresAt
	}
	cred, err := st.UpsertCredential(accountID, patch)
	if err != nil {
		log.Printf("%s bootstrap: credential upsert warning: %v", provider, err)
		return
	}
	gateway.SetAccountCredential(accountID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
}
