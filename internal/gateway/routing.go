package gateway

import (
	"regexp"
	"sort"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

// ProvidersForModel returns the set of providers allowed for modelName by the
// enabled model_routes. matched is false when no route applies (all providers
// remain eligible).
func ProvidersForModel(routes []model.ModelRoute, modelName string) (allowed map[string]bool, matched bool) {
	allowed = map[string]bool{}
	name := strings.TrimSpace(modelName)
	type scored struct {
		priority int
		provider string
	}
	var hits []scored
	for _, r := range routes {
		if !r.Enabled || strings.TrimSpace(r.Pattern) == "" || strings.TrimSpace(r.Provider) == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + r.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(name) {
			hits = append(hits, scored{priority: r.Priority, provider: strings.ToLower(strings.TrimSpace(r.Provider))})
		}
	}
	if len(hits) == 0 {
		return allowed, false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].priority < hits[j].priority })
	best := hits[0].priority
	for _, h := range hits {
		if h.priority != best {
			break
		}
		allowed[h.provider] = true
	}
	return allowed, true
}

// AccountEligibleForModel reports whether account provider may serve modelName
// given routes. When no route matches, every provider is eligible.
func AccountEligibleForModel(routes []model.ModelRoute, provider, modelName string) bool {
	allowed, matched := ProvidersForModel(routes, modelName)
	if !matched {
		return true
	}
	p := strings.ToLower(strings.TrimSpace(provider))
	if allowed[p] {
		return true
	}
	// Demo mock stays eligible so local failover still works when subscription
	// accounts are absent; production pauses mock at bootstrap.
	if p == "mock" {
		return true
	}
	return false
}
