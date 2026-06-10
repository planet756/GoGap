package source

import (
	"context"

	"gogap/internal/domain"
)

// QuoteResult holds a single fund's latest exchange quote data.
type QuoteResult struct {
	Code               string
	Name               string
	Price              *float64
	ChangePercent      *float64
	Time               string
	TurnoverRate       *float64
	TurnoverAmount     *float64
	TurnoverAmountUnit string
	MarketValue        *float64
	Tradable           bool
}

// NAVResult holds a single fund's official NAV data.
type NAVResult struct {
	Code                   string
	NAV                    *float64
	NAVDate                string
	ChangePercent          *float64
	EstimatedNAV           *float64
	EstimatedNAVTime       string
	EstimatedChangePercent *float64
	Category               domain.FundCategory
}

type MetadataResult struct {
	Code             string
	PurchaseStatus   string
	RedemptionStatus string
	PurchaseLimit    string
	FundScale        string
	FundScaleDate    string
}

// QuoteAdapter fetches latest exchange quote prices for funds.
type QuoteAdapter interface {
	FetchQuotes(ctx context.Context, secIDs []string) (map[string]QuoteResult, error)
}

// NAVAdapter fetches official net asset values for funds.
type NAVAdapter interface {
	FetchNAVs(ctx context.Context, fundCodes []string) (map[string]NAVResult, error)
}

type MetadataAdapter interface {
	FetchMetadata(ctx context.Context, fundCodes []string) (map[string]MetadataResult, error)
}

// FundPoolProvider provides the set of funds to track.
type FundPoolProvider interface {
	FundPool(ctx context.Context) ([]domain.FundSeed, error)
}
