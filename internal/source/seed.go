package source

import (
	"context"

	"gogap/internal/domain"
)

// Deprecated: SeedFundPoolProvider is retained for tests only.
// Production uses eastmoney.DiscoveryPoolProvider for dynamic fund discovery.
type SeedFundPoolProvider struct{}

func (p *SeedFundPoolProvider) FundPool(_ context.Context) ([]domain.FundSeed, error) {
	return append([]domain.FundSeed(nil), SeedFunds...), nil
}

// Deprecated: SeedFunds is retained for tests only.
// Production uses eastmoney.DiscoveryPoolProvider for dynamic fund discovery.
var SeedFunds = []domain.FundSeed{
	{
		Code:       "501018",
		Name:       "南方原油LOF",
		Category:   domain.CategoryCommodity,
		Exchange:   "SH",
		QuoteSecID: "1.501018",
		NAVCode:    "501018",
		Enabled:    true,
	},
	{
		Code:       "161129",
		Name:       "易方达原油LOF",
		Category:   domain.CategoryCommodity,
		Exchange:   "SZ",
		QuoteSecID: "0.161129",
		NAVCode:    "161129",
		Enabled:    true,
	},
	{
		Code:       "161226",
		Name:       "国投瑞银白银期货LOF",
		Category:   domain.CategoryCommodity,
		Exchange:   "SZ",
		QuoteSecID: "0.161226",
		NAVCode:    "161226",
		Enabled:    true,
	},
	{
		Code:       "160216",
		Name:       "国泰大宗商品QDII-LOF",
		Category:   domain.CategoryCommodity,
		Exchange:   "SZ",
		QuoteSecID: "0.160216",
		NAVCode:    "160216",
		Enabled:    true,
	},
	{
		Code:       "501025",
		Name:       "鹏华香港银行LOF",
		Category:   domain.CategoryQDII,
		Exchange:   "SH",
		QuoteSecID: "1.501025",
		NAVCode:    "501025",
		Enabled:    true,
	},
	{
		Code:       "164906",
		Name:       "交银中证海外中国互联网LOF",
		Category:   domain.CategoryQDII,
		Exchange:   "SZ",
		QuoteSecID: "0.164906",
		NAVCode:    "164906",
		Enabled:    true,
	},
	{
		Code:       "161725",
		Name:       "招商中证白酒指数LOF",
		Category:   domain.CategoryIndexLOF,
		Exchange:   "SZ",
		QuoteSecID: "0.161725",
		NAVCode:    "161725",
		Enabled:    true,
	},
	{
		Code:       "163407",
		Name:       "兴全沪深300指数LOF",
		Category:   domain.CategoryIndexLOF,
		Exchange:   "SZ",
		QuoteSecID: "0.163407",
		NAVCode:    "163407",
		Enabled:    true,
	},
	{
		Code:       "163402",
		Name:       "兴全趋势投资LOF",
		Category:   domain.CategoryActiveLOF,
		Exchange:   "SZ",
		QuoteSecID: "0.163402",
		NAVCode:    "163402",
		Enabled:    true,
	},
	{
		Code:       "161005",
		Name:       "富国天惠成长混合LOF",
		Category:   domain.CategoryActiveLOF,
		Exchange:   "SZ",
		QuoteSecID: "0.161005",
		NAVCode:    "161005",
		Enabled:    true,
	},
	{
		Code:       "506000",
		Name:       "南方科创板3年定开混合",
		Category:   domain.CategoryActiveLOF,
		Exchange:   "SH",
		QuoteSecID: "1.506000",
		NAVCode:    "506000",
		Enabled:    true,
	},
	{
		Code:       "506006",
		Name:       "汇添富科创板2年定开混合",
		Category:   domain.CategoryActiveLOF,
		Exchange:   "SH",
		QuoteSecID: "1.506006",
		NAVCode:    "506006",
		Enabled:    true,
	},
}
