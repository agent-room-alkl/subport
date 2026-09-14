package store

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestSeedAvailableModelsUsesChatGPTCodexCatalog(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := ensureAvailableModelsTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO available_models(id, provider, model, label, enabled, sort_order, notes)
VALUES('mdl-gpt-5-4', 'codex', 'gpt-5.4', 'GPT-5.4', 1, 120, '')
`); err != nil {
		t.Fatal(err)
	}
	if err := seedAvailableModels(db, false); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT model FROM available_models WHERE provider='codex' AND enabled=1 ORDER BY sort_order, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enabled Codex models = %v, want %v", got, want)
	}

	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM available_models WHERE id='mdl-gpt-5-4'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("retired Codex model remained enabled: %d", enabled)
	}
}
