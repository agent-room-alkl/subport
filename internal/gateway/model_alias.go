package gateway

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// ModelAliasConfig maps client-facing aliases to canonical model ids and
// ordered fallback lists used on 429/5xx.
//
// Example JSON:
//
//	{
//	  "aliases": {"sonnet": "claude-sonnet-4-5-20250929"},
//	  "fallbacks": {
//	    "claude-sonnet-4-5-20250929": ["claude-haiku-4-5-20251001"]
//	  },
//	  "max_fallback_tries": 2
//	}
type ModelAliasConfig struct {
	Aliases          map[string]string   `json:"aliases"`
	Fallbacks        map[string][]string `json:"fallbacks"`
	MaxFallbackTries int                 `json:"max_fallback_tries"`
}

var (
	modelAliasMu     sync.RWMutex
	modelAliasConfig = ModelAliasConfig{
		Aliases:          map[string]string{},
		Fallbacks:        map[string][]string{},
		MaxFallbackTries: 2,
	}
)

// SetModelAliasConfig replaces the in-memory alias/fallback table.
func SetModelAliasConfig(cfg ModelAliasConfig) {
	modelAliasMu.Lock()
	defer modelAliasMu.Unlock()
	modelAliasConfig = NormalizeModelAliasConfig(cfg)
}

// NormalizeModelAliasConfig fills the documented defaults without changing
// process-global state. HTTP handlers use this before persistence so readers
// never observe a half-saved configuration.
func NormalizeModelAliasConfig(cfg ModelAliasConfig) ModelAliasConfig {
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	if cfg.Fallbacks == nil {
		cfg.Fallbacks = map[string][]string{}
	}
	if cfg.MaxFallbackTries <= 0 {
		cfg.MaxFallbackTries = 2
	}
	return cfg
}

// GetModelAliasConfig returns a deep-ish copy of the current config.
func GetModelAliasConfig() ModelAliasConfig {
	modelAliasMu.RLock()
	defer modelAliasMu.RUnlock()
	out := ModelAliasConfig{
		Aliases:          map[string]string{},
		Fallbacks:        map[string][]string{},
		MaxFallbackTries: modelAliasConfig.MaxFallbackTries,
	}
	for k, v := range modelAliasConfig.Aliases {
		out.Aliases[k] = v
	}
	for k, vs := range modelAliasConfig.Fallbacks {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out.Fallbacks[k] = cp
	}
	return out
}

// LoadModelAliasConfigJSON unmarshals cfg JSON into the process table.
func LoadModelAliasConfigJSON(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		SetModelAliasConfig(ModelAliasConfig{MaxFallbackTries: 2})
		return nil
	}
	var cfg ModelAliasConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return err
	}
	SetModelAliasConfig(cfg)
	return nil
}

// LoadModelAliasConfigFile reads a JSON file if it exists.
func LoadModelAliasConfigFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return LoadModelAliasConfigJSON(string(b))
}

// ResolveModelAlias returns the canonical model id for name (or name itself).
func ResolveModelAlias(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	modelAliasMu.RLock()
	defer modelAliasMu.RUnlock()
	if v, ok := modelAliasConfig.Aliases[name]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	// case-insensitive alias lookup
	low := strings.ToLower(name)
	for k, v := range modelAliasConfig.Aliases {
		if strings.EqualFold(k, name) || strings.ToLower(k) == low {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return name
}

// ModelAttemptChain returns [canonical, fallback1, ...] capped by MaxFallbackTries
// additional fallbacks (so total length <= 1+MaxFallbackTries).
func ModelAttemptChain(requested string) []string {
	canonical := ResolveModelAlias(requested)
	modelAliasMu.RLock()
	defer modelAliasMu.RUnlock()
	maxExtra := modelAliasConfig.MaxFallbackTries
	if maxExtra <= 0 {
		maxExtra = 2
	}
	out := []string{canonical}
	seen := map[string]bool{strings.ToLower(canonical): true}
	fb := modelAliasConfig.Fallbacks[canonical]
	if len(fb) == 0 {
		// also try lookup by original requested alias key
		fb = modelAliasConfig.Fallbacks[requested]
	}
	for _, m := range fb {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// resolve nested aliases once
		m2 := m
		if v, ok := modelAliasConfig.Aliases[m]; ok && strings.TrimSpace(v) != "" {
			m2 = strings.TrimSpace(v)
		}
		key := strings.ToLower(m2)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m2)
		if len(out)-1 >= maxExtra {
			break
		}
	}
	return out
}
