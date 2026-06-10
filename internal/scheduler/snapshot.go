package scheduler

import (
	"fmt"
	"sort"
	"time"

	"gogap/internal/domain"
	"gogap/internal/source"
)

func (s *Service) buildSnapshots(seeds []domain.FundSeed, quotes map[string]source.QuoteResult, now time.Time, extraErrors []string) []domain.FundSnapshot {
	s.mu.RLock()
	navs := make(map[string]source.NAVResult, len(s.navs))
	for code, nav := range s.navs {
		navs[code] = nav
	}
	metadata := make(map[string]source.MetadataResult, len(s.metadataItems))
	for code, item := range s.metadataItems {
		metadata[code] = item
	}
	s.mu.RUnlock()

	items := make([]domain.FundSnapshot, 0, len(seeds))
	for _, seed := range seeds {
		officialNAV, hasNAV := navs[seed.NAVCode]
		nav := selectedNAVResult(officialNAV, now)
		quote, hasQuote := quotes[seed.QuoteSecID]
		meta := metadata[seed.Code]
		meta = fillScaleFromQuote(meta, quote)
		name := snapshotName(seed.Name, quote.Name)
		errorsForItem := cloneErrors(extraErrors)
		if !hasNAV || nav.NAV == nil || *nav.NAV <= 0 {
			errorsForItem = append(errorsForItem, "official NAV unavailable")
		}
		if !hasQuote || !quote.Tradable || quote.Price == nil || hasAbsurdQuotePremium(nav, hasNAV, quote) {
			errorsForItem = append(errorsForItem, "exchange quote unavailable")
		}
		if !isSeedSnapshotEligible(seed, nav, meta, quote, now) {
			continue
		}

		stale := s.snapshotStaleState(nav, hasNAV, quote, hasQuote, now)
		sourceState := domain.SourceStateReady
		if len(errorsForItem) > 0 {
			sourceState = domain.SourceStatePartial
		}

		var premium *float64
		if hasNAV && nav.NAV != nil && hasQuote {
			premium = domain.CalculatePremiumRate(*nav.NAV, quote.Price, quote.Tradable)
		}

		item := domain.FundSnapshot{
			Code:               seed.Code,
			Name:               name,
			Category:           snapshotCategory(seed.Category, nav.Category),
			NAV:                cloneFloat(nav.NAV),
			NAVDate:            nav.NAVDate,
			NAVBasis:           navBasis(officialNAV, now, s.navBasis),
			NAVChangePercent:   cloneFloat(nav.ChangePercent),
			QuotePrice:         cloneFloat(quote.Price),
			QuoteChangePercent: cloneFloat(quote.ChangePercent),
			QuoteTime:          quote.Time,
			PremiumRate:        premium,
			PurchaseStatus:     meta.PurchaseStatus,
			RedemptionStatus:   meta.RedemptionStatus,
			PurchaseLimit:      meta.PurchaseLimit,
			FundScale:          meta.FundScale,
			FundScaleDate:      meta.FundScaleDate,
			TurnoverRate:       cloneFloat(quote.TurnoverRate),
			TurnoverAmount:     cloneFloat(quote.TurnoverAmount),
			TurnoverAmountUnit: quote.TurnoverAmountUnit,
			InWatchlist:        false,
			Source:             sourceState,
			Stale:              stale,
			Errors:             errorsForItem,
		}
		items = append(items, item)
	}
	return items
}

func selectedNAVResult(nav source.NAVResult, now time.Time) source.NAVResult {
	if hasSameDayOfficialNAV(nav, now) || !hasUsableEstimate(nav, now) {
		return nav
	}
	selected := nav
	selected.NAV = cloneFloat(nav.EstimatedNAV)
	selected.NAVDate = nav.EstimatedNAVTime
	selected.ChangePercent = cloneFloat(nav.EstimatedChangePercent)
	return selected
}

func hasSameDayOfficialNAV(nav source.NAVResult, now time.Time) bool {
	if nav.NAV == nil || *nav.NAV <= 0 || nav.NAVDate == "" {
		return false
	}
	date, err := parseSnapshotNAVDate(nav.NAVDate)
	if err != nil {
		return false
	}
	return dateOnly(date).Equal(dateOnly(now))
}

func hasUsableEstimate(nav source.NAVResult, now time.Time) bool {
	if nav.EstimatedNAV == nil || *nav.EstimatedNAV <= 0 || nav.EstimatedNAVTime == "" {
		return false
	}
	estimateTime, err := time.ParseInLocation("2006-01-02 15:04", nav.EstimatedNAVTime, ChinaStandardTime)
	if err != nil {
		return false
	}
	return dateOnly(estimateTime).Equal(dateOnly(now))
}

func navBasis(nav source.NAVResult, now time.Time, defaultBasis string) string {
	if !hasSameDayOfficialNAV(nav, now) && hasUsableEstimate(nav, now) {
		return EstimateNAVBasis
	}
	return defaultBasis
}

func snapshotName(seedName, quoteName string) string {
	if quoteName != "" {
		return quoteName
	}
	return seedName
}

func isSeedSnapshotEligible(seed domain.FundSeed, nav source.NAVResult, meta source.MetadataResult, quote source.QuoteResult, now time.Time) bool {
	if nav.NAV == nil || *nav.NAV <= 0 || !isSnapshotTradable(seed, nav, meta) {
		return false
	}
	if quote.Tradable && quote.Price != nil {
		return !hasAbsurdQuotePremium(nav, true, quote)
	}
	return !isTradingHours(now) && (quote.Code != "" || quote.Time != "" || quote.Name != "")
}

func snapshotCategory(seedCategory, navCategory domain.FundCategory) domain.FundCategory {
	if seedCategory == domain.CategoryETF {
		return seedCategory
	}
	if navCategory != "" {
		return navCategory
	}
	return seedCategory
}

func fillScaleFromQuote(meta source.MetadataResult, quote source.QuoteResult) source.MetadataResult {
	if meta.FundScale != "" || quote.MarketValue == nil || *quote.MarketValue <= 0 {
		return meta
	}
	meta.FundScale = formatScale(*quote.MarketValue)
	return meta
}

func formatScale(value float64) string {
	return fmt.Sprintf("%.2f亿元", value/100000000)
}

func hasAbsurdQuotePremium(nav source.NAVResult, hasNAV bool, quote source.QuoteResult) bool {
	if !hasNAV || nav.NAV == nil || *nav.NAV <= 0 || quote.Price == nil || !quote.Tradable {
		return false
	}
	premium := domain.CalculatePremiumRate(*nav.NAV, quote.Price, quote.Tradable)
	return premium != nil && (*premium > 500 || *premium < -95)
}

func isOTCTradable(meta source.MetadataResult) bool {
	return isPurchaseTradable(meta.PurchaseStatus) && isRedemptionTradable(meta.RedemptionStatus)
}

func isSnapshotTradable(seed domain.FundSeed, nav source.NAVResult, meta source.MetadataResult) bool {
	if seed.Category == domain.CategoryETF && meta.PurchaseStatus == "场内交易" && meta.RedemptionStatus == "场内交易" {
		return isAllowedExchangeOnlyETFCategory(nav.Category)
	}
	return isOTCTradable(meta)
}

func isAllowedExchangeOnlyETFCategory(category domain.FundCategory) bool {
	return category == domain.CategoryQDII || category == domain.CategoryHongKong || category == domain.CategoryCommodity
}

func isPurchaseTradable(status string) bool {
	switch status {
	case "开放申购", "暂停申购", "限大额":
		return true
	default:
		return false
	}
}

func isRedemptionTradable(status string) bool {
	switch status {
	case "开放赎回", "暂停赎回":
		return true
	default:
		return false
	}
}

func (s *Service) snapshotStaleState(nav source.NAVResult, hasNAV bool, quote source.QuoteResult, hasQuote bool, now time.Time) domain.StaleState {
	if !isTradingHours(now) {
		return domain.StaleStateClosed
	}
	if !hasNAV || s.navResultStale(nav, now) || quoteStale(quote, hasQuote) {
		return domain.StaleStateStale
	}
	return domain.StaleStateFresh
}

func (s *Service) navResultStale(nav source.NAVResult, now time.Time) bool {
	if nav.NAV == nil || nav.NAVDate == "" {
		return true
	}
	date, err := parseSnapshotNAVDate(nav.NAVDate)
	if err != nil {
		return true
	}
	expected := expectedNAVDate(now)
	return date.Before(expected)
}

func quoteStale(quote source.QuoteResult, present bool) bool {
	if !present {
		return true
	}
	if !quote.Tradable || quote.Price == nil || quote.Time == "" {
		return true
	}
	return false
}

func sortSnapshots(items []domain.FundSnapshot) {
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
}

func aggregateSource(items []domain.FundSnapshot, errors []string) domain.SourceState {
	if len(errors) > 0 {
		return domain.SourceStateError
	}
	if len(items) == 0 {
		return domain.SourceStateLoading
	}
	for _, item := range items {
		if item.Source == domain.SourceStateError {
			return domain.SourceStateError
		}
	}
	return domain.SourceStateReady
}

func aggregateStale(items []domain.FundSnapshot) domain.StaleState {
	if len(items) == 0 {
		return domain.StaleStateUnknown
	}
	state := domain.StaleStateStale
	for _, item := range items {
		if item.Stale == domain.StaleStateFresh {
			return domain.StaleStateFresh
		}
		if item.Stale == domain.StaleStateClosed {
			state = domain.StaleStateClosed
		}
	}
	return state
}
