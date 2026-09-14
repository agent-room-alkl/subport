package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

const availableModelsDDL = `
CREATE TABLE IF NOT EXISTS available_models (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_available_models_sort ON available_models(enabled, sort_order, id);
`

func ensureAvailableModelsTable(db *sql.DB) error {
	_, err := db.Exec(availableModelsDDL)
	return err
}

func ensureAvailableModelsTablePG(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS available_models (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  enabled SMALLINT NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_available_models_sort ON available_models(enabled, sort_order, id);
`)
	return err
}

type availableModelSeed struct {
	id, provider, model, label, notes string
	sort                              int
	enabled                           bool
}

func defaultAvailableModelSeeds() []availableModelSeed {
	return []availableModelSeed{
		{"mdl-claude-fable-5-1", "claude", "claude-fable-5-1", "Fable 5.1", "Requires usage credits", 10, true},
		{"mdl-claude-sonnet-5", "claude", "claude-sonnet-5", "Sonnet 5", "", 20, true},
		{"mdl-claude-haiku-4-5", "claude", "claude-haiku-4-5-20251001", "Haiku 4.5", "", 30, true},
		{"mdl-claude-fable-5", "claude", "claude-fable-5", "Fable 5", "Requires usage credits", 40, true},
		{"mdl-claude-opus-4-8", "claude", "claude-opus-4-8", "Opus 4.8", "", 50, true},
		{"mdl-claude-opus-4-7", "claude", "claude-opus-4-7", "Opus 4.7", "", 60, true},
		{"mdl-claude-opus-4-6", "claude", "claude-opus-4-6", "Opus 4.6", "", 70, true},
		{"mdl-claude-opus-3", "claude", "claude-3-opus-20240229", "Opus 3", "Legacy; may be unavailable", 80, false},
		{"mdl-claude-sonnet-4-6", "claude", "claude-sonnet-4-6", "Sonnet 4.6", "", 90, true},
		{"mdl-gpt-6-astra", "codex", "gpt-6-astra", "GPT-6-Astra", "Our most capable model for complex, demanding work", 100, true},
		{"mdl-gpt-5-6-sol", "codex", "gpt-5.6-sol", "GPT-5.6-Sol", "Reliable agentic workhorse for everyday tasks", 110, true},
		{"mdl-gpt-5-6-terra", "codex", "gpt-5.6-terra", "GPT-5.6-Terra", "Balanced agentic coding model for everyday work", 120, true},
		{"mdl-gpt-5-6-luna", "codex", "gpt-5.6-luna", "GPT-5.6-Luna", "Fast and affordable agentic coding model", 130, true},
		{"mdl-gpt-5-5", "codex", "gpt-5.5", "GPT-5.5", "Proven previous-generation model for coding and general work", 140, true},
		{"mdl-gemini-2-5-flash", "antigravity", "gemini-2.5-flash", "Gemini 2.5 Flash", "Disabled until healthy accounts", 250, false},
		{"mdl-claude-sonnet-4-5", "claude", "claude-sonnet-4-5", "Sonnet 4.5 (legacy)", "Legacy; disabled", 260, false},
	}
}

func seedAvailableModels(db *sql.DB, postgres bool) error {
	for _, d := range defaultAvailableModelSeeds() {
		en := 0
		if d.enabled {
			en = 1
		}
		var err error
		if postgres {
			_, err = db.Exec(`
INSERT INTO available_models(id, provider, model, label, enabled, sort_order, notes)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO NOTHING
`, d.id, d.provider, d.model, d.label, en, d.sort, d.notes)
		} else {
			_, err = db.Exec(`
INSERT OR IGNORE INTO available_models(id, provider, model, label, enabled, sort_order, notes)
VALUES(?,?,?,?,?,?,?)
`, d.id, d.provider, d.model, d.label, en, d.sort, d.notes)
		}
		if err != nil {
			return err
		}
	}
	if err := reconcileCodexModelCatalog(db, postgres); err != nil {
		return err
	}
	return disableRetiredCodexModels(db, postgres)
}

// Existing installations already have the gpt-5.5 seed. Refresh built-in
// Codex metadata and ordering while preserving the administrator's enabled
// choice for models that remain supported.
func reconcileCodexModelCatalog(db *sql.DB, postgres bool) error {
	for _, d := range defaultAvailableModelSeeds() {
		if d.provider != "codex" {
			continue
		}
		if postgres {
			if _, err := db.Exec(`
UPDATE available_models
SET provider=$2, model=$3, label=$4, sort_order=$5, notes=$6
WHERE id=$1
`, d.id, d.provider, d.model, d.label, d.sort, d.notes); err != nil {
				return err
			}
			continue
		}
		if _, err := db.Exec(`
UPDATE available_models
SET provider=?, model=?, label=?, sort_order=?, notes=?
WHERE id=?
`, d.provider, d.model, d.label, d.sort, d.notes, d.id); err != nil {
			return err
		}
	}
	return nil
}

// These entries were copied from the OpenAI Platform model catalog, but this
// provider authenticates against the ChatGPT Codex backend. That backend
// rejects them with "not supported when using Codex with a ChatGPT account".
// Keep existing rows for auditability and admin visibility, but stop offering
// them by default on databases created before the catalog was corrected.
func disableRetiredCodexModels(db *sql.DB, postgres bool) error {
	ids := []string{
		"mdl-gpt-5-5-pro", "mdl-gpt-5-4", "mdl-gpt-5-4-mini",
		"mdl-gpt-5-4-nano", "mdl-gpt-5-4-pro", "mdl-gpt-5-3-codex",
		"mdl-gpt-5-2-codex", "mdl-gpt-5-codex", "mdl-gpt-5-codex-mini",
		"mdl-gpt-4-1", "mdl-gpt-4-1-mini", "mdl-o3", "mdl-o4-mini",
		"mdl-codex-mini",
	}
	placeholder := "?"
	if postgres {
		placeholder = "$1"
	}
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE available_models SET enabled=0 WHERE id=`+placeholder+` AND provider='codex'`, id); err != nil {
			return err
		}
	}
	return nil
}

func scanAvailableModel(scanner interface{ Scan(...any) error }) (model.AvailableModel, error) {
	var m model.AvailableModel
	var en int
	if err := scanner.Scan(&m.ID, &m.Provider, &m.Model, &m.Label, &en, &m.SortOrder, &m.Notes); err != nil {
		return m, err
	}
	m.Enabled = en != 0
	return m, nil
}

func (s *Store) sqliteListAvailableModels(enabledOnly bool) ([]model.AvailableModel, error) {
	q := `SELECT id, provider, model, label, enabled, sort_order, notes FROM available_models`
	if enabledOnly {
		q += ` WHERE enabled=1`
	}
	q += ` ORDER BY sort_order, id`
	rows, err := s.query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AvailableModel{}
	for rows.Next() {
		m, err := scanAvailableModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) sqliteSetAvailableModelEnabled(id string, enabled bool) error {
	res, err := s.exec(`UPDATE available_models SET enabled=? WHERE id=?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) sqliteUpsertAvailableModel(m model.AvailableModel) error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("id required")
	}
	_, err := s.exec(`
INSERT INTO available_models(id, provider, model, label, enabled, sort_order, notes)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  provider=excluded.provider,
  model=excluded.model,
  label=excluded.label,
  enabled=excluded.enabled,
  sort_order=excluded.sort_order,
  notes=excluded.notes
`, m.ID, m.Provider, m.Model, m.Label, boolInt(m.Enabled), m.SortOrder, m.Notes)
	return err
}

// UsageUserStat aggregates usage for one user.
type UsageUserStat struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Calls        int64  `json:"calls"`
	Tokens       int64  `json:"tokens"`
	Cost         int64  `json:"cost"`
	SuccessCalls int64  `json:"success_calls"`
	FailedCalls  int64  `json:"failed_calls"`
}

// UsageModelCostStat aggregates usage for one model (with cost).
type UsageModelCostStat struct {
	Model  string `json:"model"`
	Calls  int64  `json:"calls"`
	Tokens int64  `json:"tokens"`
	Cost   int64  `json:"cost"`
}

// UsageUserModelStat aggregates usage for one username+model pair.
type UsageUserModelStat struct {
	Username string `json:"username"`
	Model    string `json:"model"`
	Calls    int64  `json:"calls"`
	Tokens   int64  `json:"tokens"`
	Cost     int64  `json:"cost"`
}

// UsageSummary is the admin monitoring rollup.
type UsageSummary struct {
	PerUser      []UsageUserStat      `json:"per_user"`
	PerModel     []UsageModelCostStat `json:"per_model"`
	PerUserModel []UsageUserModelStat `json:"per_user_model"`
}

func (s *Store) sqliteUsageSummary(topN int) (UsageSummary, error) {
	if topN <= 0 {
		topN = 20
	}
	out := UsageSummary{
		PerUser:      []UsageUserStat{},
		PerModel:     []UsageModelCostStat{},
		PerUserModel: []UsageUserModelStat{},
	}

	rows, err := s.query(`
SELECT COALESCE(u.id, l.user_id) AS user_id,
       COALESCE(u.username, '') AS username,
       COUNT(*) AS calls,
       COALESCE(SUM(l.tokens), 0) AS tokens,
       COALESCE(SUM(l.cost), 0) AS cost,
       COALESCE(SUM(CASE WHEN l.status = 'success' THEN 1 ELSE 0 END), 0) AS success_calls,
       COALESCE(SUM(CASE WHEN l.status <> 'success' THEN 1 ELSE 0 END), 0) AS failed_calls
FROM usage_logs l
LEFT JOIN users u ON u.id = l.user_id
GROUP BY COALESCE(u.id, l.user_id), COALESCE(u.username, '')
ORDER BY calls DESC
`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var st UsageUserStat
		if err := rows.Scan(&st.UserID, &st.Username, &st.Calls, &st.Tokens, &st.Cost, &st.SuccessCalls, &st.FailedCalls); err != nil {
			rows.Close()
			return out, err
		}
		out.PerUser = append(out.PerUser, st)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.query(`
SELECT model,
       COUNT(*) AS calls,
       COALESCE(SUM(tokens), 0) AS tokens,
       COALESCE(SUM(cost), 0) AS cost
FROM usage_logs
GROUP BY model
ORDER BY calls DESC
`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var st UsageModelCostStat
		if err := rows.Scan(&st.Model, &st.Calls, &st.Tokens, &st.Cost); err != nil {
			rows.Close()
			return out, err
		}
		out.PerModel = append(out.PerModel, st)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.query(`
SELECT COALESCE(u.username, '') AS username,
       l.model,
       COUNT(*) AS calls,
       COALESCE(SUM(l.tokens), 0) AS tokens,
       COALESCE(SUM(l.cost), 0) AS cost
FROM usage_logs l
LEFT JOIN users u ON u.id = l.user_id
GROUP BY COALESCE(u.username, ''), l.model
ORDER BY calls DESC
LIMIT ?
`, topN)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var st UsageUserModelStat
		if err := rows.Scan(&st.Username, &st.Model, &st.Calls, &st.Tokens, &st.Cost); err != nil {
			return out, err
		}
		out.PerUserModel = append(out.PerUserModel, st)
	}
	return out, rows.Err()
}
