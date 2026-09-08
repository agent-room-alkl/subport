package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func TestT12CKeysQuotaUsageSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t12c-smoke.json")

	s, err := store.OpenStore(path, "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()
	if _, err := os.Stat(path + ".sqlite"); err != nil {
		t.Fatalf("expected sqlite file: %v", err)
	}

	alice, err := s.CreateUser("alice", "pw", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob", "pw", model.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	ak, secret, err := s.CreateKey(alice.ID, "alice-key")
	if err != nil {
		t.Fatalf("CreateKey alice: %v", err)
	}
	if ak.Prefix == "" || ak.Prefix != secret[:12] {
		t.Fatalf("prefix want %q got %q", secret[:12], ak.Prefix)
	}
	bk, _, err := s.CreateKey(bob.ID, "bob-key")
	if err != nil {
		t.Fatalf("CreateKey bob: %v", err)
	}

	found, owner, err := s.KeyBySecret(secret)
	if err != nil {
		t.Fatalf("KeyBySecret: %v", err)
	}
	if found.ID != ak.ID || owner.ID != alice.ID {
		t.Fatalf("KeyBySecret mismatch key=%s owner=%s", found.ID, owner.ID)
	}

	aliceKeys := s.KeysOf(alice.ID)
	if len(aliceKeys) != 1 || aliceKeys[0].ID != ak.ID {
		t.Fatalf("alice KeysOf = %+v", aliceKeys)
	}
	for _, k := range aliceKeys {
		if k.ID == bk.ID {
			t.Fatal("alice must not see bob key")
		}
	}
	bobKeys := s.KeysOf(bob.ID)
	if len(bobKeys) != 1 || bobKeys[0].ID != bk.ID {
		t.Fatalf("bob KeysOf = %+v", bobKeys)
	}

	if _, err := s.KeyByID(alice.ID, bk.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("alice KeyByID bob want ErrNotFound got %v", err)
	}
	if err := s.SetKeyEnabled(alice.ID, bk.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("alice SetKeyEnabled bob want ErrNotFound got %v", err)
	}
	if err := s.DeleteKey(alice.ID, bk.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("alice DeleteKey bob want ErrNotFound got %v", err)
	}

	if err := s.AddUsage(model.UsageLog{
		UserID: alice.ID, KeyID: ak.ID, Model: "demo", Tokens: 10, Cost: 42,
		Status: "stream_broken", AccountID: "acct-1", Attempts: 1, StreamBroken: true,
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	u, err := s.UserByID(alice.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.QuotaUsed != 42 {
		t.Fatalf("quota_used want 42 got %d", u.QuotaUsed)
	}
	usage := s.UsageOf(alice.ID, 10)
	if len(usage) != 1 || !usage[0].StreamBroken || usage[0].Compensated {
		t.Fatalf("UsageOf stream fields = %+v", usage)
	}

	s.TouchKey(ak.ID, "2026-09-08T11:00:00Z")
	touched, err := s.KeyByID(alice.ID, ak.ID)
	if err != nil || touched.LastUsed != "2026-09-08T11:00:00Z" {
		t.Fatalf("TouchKey last_used=%q err=%v", touched.LastUsed, err)
	}

	if err := s.SetKeyEnabled(alice.ID, ak.ID, false); err != nil {
		t.Fatalf("SetKeyEnabled: %v", err)
	}
	if _, _, err := s.KeyBySecret(secret); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disabled key should 404, got %v", err)
	}
	if err := s.SetKeyEnabled(alice.ID, ak.ID, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}

	// Restart: reopen and confirm SQLite persistence.
	_ = s.Close()
	s2, err := store.OpenStore(path, "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	keys2 := s2.KeysOf(alice.ID)
	if len(keys2) != 1 || keys2[0].ID != ak.ID || keys2[0].Prefix != ak.Prefix {
		t.Fatalf("after reopen KeysOf=%+v", keys2)
	}
	u2, err := s2.UserByID(alice.ID)
	if err != nil || u2.QuotaUsed != 42 {
		t.Fatalf("after reopen quota=%d err=%v", u2.QuotaUsed, err)
	}
	usage2 := s2.UsageOf(alice.ID, 10)
	if len(usage2) != 1 || !usage2[0].StreamBroken || usage2[0].Compensated {
		t.Fatalf("after reopen usage=%+v", usage2)
	}
	found2, _, err := s2.KeyBySecret(secret)
	if err != nil || found2.ID != ak.ID {
		t.Fatalf("after reopen KeyBySecret: %v %+v", err, found2)
	}

	// Compensation path uses SQLite usage + quota credit.
	cfg := model.CompensationConfig{Threshold: 0.1, WindowSize: 10, MinBroken: 1}
	res, err := s2.CompensateBrokenStreams(alice.ID, cfg)
	if err != nil {
		t.Fatalf("CompensateBrokenStreams: %v", err)
	}
	if !res.Triggered || res.Credited != 42 || res.CompensatedCount != 1 {
		t.Fatalf("compensation result=%+v", res)
	}
	u3, _ := s2.UserByID(alice.ID)
	if u3.QuotaUsed != 0 {
		t.Fatalf("after compensate quota_used want 0 got %d", u3.QuotaUsed)
	}
	pending, completed := s2.Compensations()
	if len(pending) != 0 || len(completed) != 1 || !completed[0].Compensated || !completed[0].StreamBroken {
		t.Fatalf("Compensations pending=%+v completed=%+v", pending, completed)
	}

	if err := s2.DeleteKey(alice.ID, ak.ID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if len(s2.KeysOf(alice.ID)) != 0 {
		t.Fatal("expected no keys after delete")
	}
}
