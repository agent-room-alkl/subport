-- Default seed data. Idempotent (ON CONFLICT DO NOTHING); safe to re-run.
-- Mirrors seedDefaultModelRoutesPG and defaultAvailableModelSeeds in
-- internal/store. The Go process seeds these on first startup too; this file
-- lets you populate a fresh database ahead of time.
--
--   psql "host=<host> user=<admin> dbname=subport sslmode=require" -f 02_seed.sql

-- Model routing: which provider handles a model name (first match wins by priority).
INSERT INTO model_routes (id, pattern, provider, priority, enabled) VALUES
  ('route-claude',      '^claude',                  'claude',      1, 1),
  ('route-codex',       '^gpt-|^o[0-9]|^codex',     'codex',       1, 1),
  ('route-antigravity', '^gemini|^tab_flash|^gpt-oss','antigravity',1, 1)
ON CONFLICT (id) DO NOTHING;

-- Catalog of models offered to users (enabled=1 shown, 0 hidden).
INSERT INTO available_models (id, provider, model, label, enabled, sort_order, notes) VALUES
  ('mdl-claude-fable-5-1', 'claude',      'claude-fable-5-1',           'Fable 5.1',          1, 10,  'Requires usage credits'),
  ('mdl-claude-sonnet-5',  'claude',      'claude-sonnet-5',            'Sonnet 5',           1, 20,  ''),
  ('mdl-claude-haiku-4-5', 'claude',      'claude-haiku-4-5-20251001',  'Haiku 4.5',          1, 30,  ''),
  ('mdl-claude-fable-5',   'claude',      'claude-fable-5',             'Fable 5',            1, 40,  'Requires usage credits'),
  ('mdl-claude-opus-4-8',  'claude',      'claude-opus-4-8',            'Opus 4.8',           1, 50,  ''),
  ('mdl-claude-opus-4-7',  'claude',      'claude-opus-4-7',            'Opus 4.7',           1, 60,  ''),
  ('mdl-claude-opus-4-6',  'claude',      'claude-opus-4-6',            'Opus 4.6',           1, 70,  ''),
  ('mdl-claude-opus-3',    'claude',      'claude-3-opus-20240229',     'Opus 3',             0, 80,  'Legacy; may be unavailable'),
  ('mdl-claude-sonnet-4-6','claude',      'claude-sonnet-4-6',          'Sonnet 4.6',         1, 90,  ''),
  ('mdl-gpt-6-astra',      'codex',       'gpt-6-astra',                'GPT-6-Astra',        1, 100, 'Our most capable model for complex, demanding work'),
  ('mdl-gpt-5-6-sol',      'codex',       'gpt-5.6-sol',                'GPT-5.6-Sol',        1, 110, 'Reliable agentic workhorse for everyday tasks'),
  ('mdl-gpt-5-6-terra',    'codex',       'gpt-5.6-terra',              'GPT-5.6-Terra',      1, 120, 'Balanced agentic coding model for everyday work'),
  ('mdl-gpt-5-6-luna',     'codex',       'gpt-5.6-luna',               'GPT-5.6-Luna',       1, 130, 'Fast and affordable agentic coding model'),
  ('mdl-gpt-5-5',          'codex',       'gpt-5.5',                    'GPT-5.5',            1, 140, 'Proven previous-generation model for coding and general work'),
  ('mdl-gemini-2-5-flash', 'antigravity', 'gemini-2.5-flash',           'Gemini 2.5 Flash',   0, 250, 'Disabled until healthy accounts'),
  ('mdl-claude-sonnet-4-5','claude',      'claude-sonnet-4-5',          'Sonnet 4.5 (legacy)',0, 260, 'Legacy; disabled')
ON CONFLICT (id) DO NOTHING;
