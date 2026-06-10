package domain

type FundCategory string

const (
	CategoryQDII      FundCategory = "QDII"
	CategoryActiveLOF FundCategory = "主动LOF"
	CategoryIndexLOF  FundCategory = "指数LOF"
	CategoryOtherLOF  FundCategory = "其他LOF"
	CategoryHongKong  FundCategory = "香港"
	CategoryCommodity FundCategory = "商品"
	CategoryETF       FundCategory = "ETF"
	CategoryBondMoney FundCategory = "债券货币"
)

var RequiredCategories = []FundCategory{
	CategoryQDII,
	CategoryCommodity,
	CategoryHongKong,
	CategoryActiveLOF,
	CategoryIndexLOF,
	CategoryETF,
	CategoryBondMoney,
}

type FundSeed struct {
	Code           string       `json:"code"`
	Name           string       `json:"name"`
	Category       FundCategory `json:"category"`
	Exchange       string       `json:"exchange"`
	QuoteSecID     string       `json:"quoteSecID"`
	NAVCode        string       `json:"navCode"`
	Enabled        bool         `json:"enabled"`
	DisabledReason string       `json:"disabledReason,omitempty"`
}

type SourceState string

const (
	SourceStateLoading SourceState = "loading"
	SourceStateReady   SourceState = "ready"
	SourceStatePartial SourceState = "partial"
	SourceStateError   SourceState = "error"
)

type StaleState string

const (
	StaleStateUnknown StaleState = "unknown"
	StaleStateFresh   StaleState = "fresh"
	StaleStateStale   StaleState = "stale"
	StaleStateClosed  StaleState = "closed"
)

type FundSnapshot struct {
	Code               string       `json:"code"`
	Name               string       `json:"name"`
	Category           FundCategory `json:"category"`
	NAV                *float64     `json:"nav"`
	NAVDate            string       `json:"navDate"`
	NAVBasis           string       `json:"navBasis"`
	NAVChangePercent   *float64     `json:"navChangePercent"`
	QuotePrice         *float64     `json:"quotePrice"`
	QuoteChangePercent *float64     `json:"quoteChangePercent"`
	QuoteTime          string       `json:"quoteTime"`
	PremiumRate        *float64     `json:"premiumRate"`
	PurchaseStatus     string       `json:"purchaseStatus"`
	RedemptionStatus   string       `json:"redemptionStatus"`
	PurchaseLimit      string       `json:"purchaseLimit"`
	FundScale          string       `json:"fundScale"`
	FundScaleDate      string       `json:"fundScaleDate"`
	TurnoverRate       *float64     `json:"turnoverRate"`
	TurnoverAmount     *float64     `json:"turnoverAmount"`
	TurnoverAmountUnit string       `json:"turnoverAmountUnit"`
	InWatchlist        bool         `json:"inWatchlist"`
	Source             SourceState  `json:"source"`
	Stale              StaleState   `json:"stale"`
	Errors             []string     `json:"errors"`
}

type SnapshotResponse struct {
	Disclaimer string         `json:"disclaimer"`
	Items      []FundSnapshot `json:"items"`
	Source     SourceState    `json:"source"`
	Stale      StaleState     `json:"stale"`
	Errors     []string       `json:"errors"`
	Progress   *ProgressState `json:"progress"`
}

type ProgressState struct {
	Label   string `json:"label"`
	Percent int    `json:"percent"`
}

type WatchlistItem struct {
	Code      string `json:"code"`
	CreatedAt string `json:"createdAt"`
}

func NormalizeSnapshotResponse(snapshot SnapshotResponse) SnapshotResponse {
	if snapshot.Items == nil {
		snapshot.Items = []FundSnapshot{}
	}
	for i := range snapshot.Items {
		snapshot.Items[i] = NormalizeFundSnapshot(snapshot.Items[i])
	}
	if snapshot.Errors == nil {
		snapshot.Errors = []string{}
	}
	return snapshot
}

func NormalizeFundSnapshot(snapshot FundSnapshot) FundSnapshot {
	if snapshot.Errors == nil {
		snapshot.Errors = []string{}
	}
	return snapshot
}

func CalculatePremiumRate(officialNav float64, latestExchangePrice *float64, quoteTradable bool) *float64 {
	if officialNav <= 0 || latestExchangePrice == nil || !quoteTradable {
		return nil
	}

	premiumRate := ((*latestExchangePrice - officialNav) / officialNav) * 100
	return &premiumRate
}
