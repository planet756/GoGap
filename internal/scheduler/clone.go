package scheduler

import (
	"gogap/internal/domain"
	"gogap/internal/source"
)

func cloneSnapshots(items []domain.FundSnapshot) []domain.FundSnapshot {
	if items == nil {
		return []domain.FundSnapshot{}
	}
	cloned := make([]domain.FundSnapshot, len(items))
	for i, item := range items {
		cloned[i] = item
		cloned[i].NAV = cloneFloat(item.NAV)
		cloned[i].NAVChangePercent = cloneFloat(item.NAVChangePercent)
		cloned[i].QuotePrice = cloneFloat(item.QuotePrice)
		cloned[i].QuoteChangePercent = cloneFloat(item.QuoteChangePercent)
		cloned[i].PremiumRate = cloneFloat(item.PremiumRate)
		cloned[i].TurnoverRate = cloneFloat(item.TurnoverRate)
		cloned[i].TurnoverAmount = cloneFloat(item.TurnoverAmount)
		cloned[i].Errors = cloneErrors(item.Errors)
	}
	return cloned
}

func cloneErrors(errors []string) []string {
	if errors == nil {
		return []string{}
	}
	return append([]string{}, errors...)
}

func cloneProgress(progress *domain.ProgressState) *domain.ProgressState {
	if progress == nil {
		return nil
	}
	cloned := *progress
	return &cloned
}

func cloneQuotes(quotes map[string]source.QuoteResult) map[string]source.QuoteResult {
	if quotes == nil {
		return map[string]source.QuoteResult{}
	}
	cloned := make(map[string]source.QuoteResult, len(quotes))
	for secID, quote := range quotes {
		cloned[secID] = source.QuoteResult{
			Code:               quote.Code,
			Name:               quote.Name,
			Price:              cloneFloat(quote.Price),
			ChangePercent:      cloneFloat(quote.ChangePercent),
			Time:               quote.Time,
			TurnoverRate:       cloneFloat(quote.TurnoverRate),
			TurnoverAmount:     cloneFloat(quote.TurnoverAmount),
			TurnoverAmountUnit: quote.TurnoverAmountUnit,
			MarketValue:        cloneFloat(quote.MarketValue),
			Tradable:           quote.Tradable,
		}
	}
	return cloned
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func appendError(existing []string, message string) []string {
	for _, item := range existing {
		if item == message {
			return existing
		}
	}
	return append(existing, message)
}

// snapshotsEqual compares two snapshot slices field by field to avoid reflect.DeepEqual.
func snapshotsEqual(a, b []domain.FundSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !snapshotEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func snapshotEqual(a, b domain.FundSnapshot) bool {
	if a.Code != b.Code || a.Name != b.Name || a.Category != b.Category {
		return false
	}
	if !floatPtrEqual(a.NAV, b.NAV) || a.NAVDate != b.NAVDate || a.NAVBasis != b.NAVBasis {
		return false
	}
	if !floatPtrEqual(a.NAVChangePercent, b.NAVChangePercent) {
		return false
	}
	if !floatPtrEqual(a.QuotePrice, b.QuotePrice) || !floatPtrEqual(a.QuoteChangePercent, b.QuoteChangePercent) {
		return false
	}
	if a.QuoteTime != b.QuoteTime {
		return false
	}
	if !floatPtrEqual(a.PremiumRate, b.PremiumRate) {
		return false
	}
	if a.PurchaseStatus != b.PurchaseStatus || a.RedemptionStatus != b.RedemptionStatus || a.PurchaseLimit != b.PurchaseLimit {
		return false
	}
	if a.FundScale != b.FundScale || a.FundScaleDate != b.FundScaleDate {
		return false
	}
	if !floatPtrEqual(a.TurnoverRate, b.TurnoverRate) || !floatPtrEqual(a.TurnoverAmount, b.TurnoverAmount) {
		return false
	}
	if a.TurnoverAmountUnit != b.TurnoverAmountUnit || a.InWatchlist != b.InWatchlist {
		return false
	}
	if a.Source != b.Source || a.Stale != b.Stale {
		return false
	}
	if len(a.Errors) != len(b.Errors) {
		return false
	}
	for j := range a.Errors {
		if a.Errors[j] != b.Errors[j] {
			return false
		}
	}
	return true
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func errorsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
