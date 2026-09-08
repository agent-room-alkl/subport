package model

// Pricing.
//
// A real price list prices token CLASSES separately - input, output, cache
// read, cache write - multiplies by a group ratio, and lets an individual
// account override that ratio. Our billing had one `tokens` number and a 1:1
// rate, which cannot express any of that. Not "expresses it badly": cannot
// represent it at all, so we could not price against a competitor, migrate a
// customer off one, or explain a bill to anyone.
//
// Everything here is integer arithmetic. Money in floats is how rounding
// errors become customer complaints that nobody can reproduce.

// RatioScale is the fixed-point scale for ratios: 10000 = 1.0x, 1600 = 0.16x.
// Basis points, because the ratios in the wild are quoted to two decimals.
const RatioScale int64 = 10000

// RateScale is the token count a rate is quoted against. Rates are "quota
// units per million tokens", which is how every provider publishes them, so
// no conversion happens between reading a price list and storing it.
const RateScale int64 = 1_000_000

// TokenCounts is what a call actually consumed, split by class.
//
// Total is authoritative when the parts are all zero: some providers report
// only a total, and inventing a breakdown from it would look precise while
// being made up. An honest total beats a fabricated split.
type TokenCounts struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Total      int64 `json:"total"`
}

// HasBreakdown reports whether the parts carry real information.
func (t TokenCounts) HasBreakdown() bool {
	return t.Input != 0 || t.Output != 0 || t.CacheRead != 0 || t.CacheWrite != 0
}

// SumParts is what the parts add up to; a row whose parts do not sum to Total
// is wrong and should be rejected rather than quietly billed.
func (t TokenCounts) SumParts() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// ModelPrice is the rate card for one model, in quota units per million
// tokens of each class.
type ModelPrice struct {
	Model      string `json:"model"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
}

// FlatPrice is the rate card for a model with no breakdown available: every
// class is charged at the same rate, so a total-only report still bills
// correctly instead of billing zero.
func FlatPrice(model string, perMillion int64) ModelPrice {
	return ModelPrice{
		Model: model, Input: perMillion, Output: perMillion,
		CacheRead: perMillion, CacheWrite: perMillion,
	}
}

// BilledRate is the price a specific call was charged at, copied onto the
// usage row.
//
// This is the part that is easy to get wrong and expensive to discover late:
// the row stores the NUMBERS, not a pointer to the price list. A bill is a
// statement about the past. If a row only referenced a price-list id, editing
// today's prices would silently rewrite every invoice ever issued, and nobody
// would find out until a customer compared two exports of the same month.
type BilledRate struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Ratio      int64 `json:"ratio"` // in RatioScale units
}

// RateFor builds the snapshot for a call: the model's rates and the effective
// ratio, frozen at the moment of billing.
func RateFor(p ModelPrice, ratio int64) BilledRate {
	return BilledRate{
		Input: p.Input, Output: p.Output,
		CacheRead: p.CacheRead, CacheWrite: p.CacheWrite,
		Ratio: ratio,
	}
}

// Cost computes what a call costs, in whole quota units.
//
// ROUNDING RULE, stated because fractional quota is exactly where money
// quietly appears and disappears: the ratio and the per-million division are
// applied together, ONCE, at the end, rounded half away from zero. Rounding
// each class separately would let four half-units become two whole ones on a
// call that consumed almost nothing.
//
// Costs are never negative: a negative rate or ratio is a configuration
// mistake, and charging a negative amount would credit quota out of thin air.
func Cost(counts TokenCounts, rate BilledRate) int64 {
	billable := counts
	if !counts.HasBreakdown() {
		// Total-only report: charge the whole thing at the input rate, which
		// FlatPrice makes equal to every other class anyway. Splitting a total
		// by a guess is what we are refusing to do.
		billable = TokenCounts{Input: counts.Total}
	}

	num := billable.Input*rate.Input +
		billable.Output*rate.Output +
		billable.CacheRead*rate.CacheRead +
		billable.CacheWrite*rate.CacheWrite

	ratio := rate.Ratio
	if ratio < 0 {
		ratio = 0
	}
	if num < 0 {
		num = 0
	}

	denom := RateScale * RatioScale
	scaled := num * ratio
	// Round half away from zero. scaled and denom are both non-negative here.
	return (scaled + denom/2) / denom
}
