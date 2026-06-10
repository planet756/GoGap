package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"gogap/internal/domain"
)

type DiscoveryPoolProvider struct {
	fetcher DiscoveryFetcher
}

type DiscoveryFetcher interface {
	FetchFundPool(ctx context.Context) ([]domain.FundSeed, error)
}

type DiscoveryAdapter struct {
	client *Client
}

type CategorySource string

const (
	CategorySourceETF CategorySource = "ETF"
	CategorySourceLOF CategorySource = "LOF"

	etfDiscoveryFS    = "b:MK0021,b:MK0022,b:MK0023,b:MK0024,b:MK0827,m:0 t:12,m:1 t:12"
	lofDiscoveryFS    = "b:MK0404,b:MK0405,b:MK0406,b:MK0407"
	discoveryPageSize = 100
)

type discoveryPayload struct {
	Code int `json:"rc"`
	Data struct {
		Total int              `json:"total"`
		Diff  []discoveryEntry `json:"diff"`
	} `json:"data"`
}

type discoveryEntry struct {
	Code   string          `json:"f12"`
	Market json.RawMessage `json:"f13"`
	Name   string          `json:"f14"`
}

func NewDiscoveryPoolProvider(fetcher DiscoveryFetcher) *DiscoveryPoolProvider {
	return &DiscoveryPoolProvider{fetcher: fetcher}
}

func (p *DiscoveryPoolProvider) FundPool(ctx context.Context) ([]domain.FundSeed, error) {
	if p.fetcher == nil {
		return nil, errors.New("eastmoney: fund discovery source missing")
	}
	seeds, err := p.fetcher.FetchFundPool(ctx)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return nil, errors.New("eastmoney: fund discovery data missing")
	}
	return seeds, nil
}

func NewDiscoveryAdapter(client *Client) *DiscoveryAdapter {
	if client == nil {
		client = NewClient(Options{})
	}
	return &DiscoveryAdapter{client: client}
}

func (a *DiscoveryAdapter) FetchFundPool(ctx context.Context) ([]domain.FundSeed, error) {
	seeds := []domain.FundSeed{}
	for _, request := range []struct {
		source CategorySource
		fs     string
	}{
		{source: CategorySourceLOF, fs: lofDiscoveryFS},
		{source: CategorySourceETF, fs: etfDiscoveryFS},
	} {
		discoveredSeeds, err := a.fetchDiscoveryPage(ctx, request.source, request.fs)
		if err != nil {
			return nil, err
		}
		seeds = mergeDiscoveredSeeds(seeds, discoveredSeeds)
	}
	if a.shouldFetchLatestNAVDiscovery() {
		latestSeeds, err := a.fetchLatestNAVDiscovery(ctx)
		if err != nil {
			return nil, err
		}
		seeds = mergeDiscoveredSeeds(seeds, latestSeeds)
	}

	if len(seeds) == 0 {
		return nil, errors.New("eastmoney: fund discovery data missing")
	}
	return seeds, nil
}

func mergeDiscoveredSeeds(base []domain.FundSeed, next []domain.FundSeed) []domain.FundSeed {
	items := make(map[string]domain.FundSeed, len(base)+len(next))
	for _, seed := range base {
		items[seed.Code] = seed
	}
	for _, seed := range next {
		if current, ok := items[seed.Code]; ok && current.Category == domain.CategoryETF {
			continue
		}
		items[seed.Code] = seed
	}
	seeds := make([]domain.FundSeed, 0, len(items))
	for _, seed := range items {
		seeds = append(seeds, seed)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Code < seeds[j].Code })
	return seeds
}

func (a *DiscoveryAdapter) shouldFetchLatestNAVDiscovery() bool {
	for _, rawURL := range a.client.quoteBaseURLs {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		if strings.HasSuffix(host, "eastmoney.com") {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
			return true
		}
	}
	return false
}

func (a *DiscoveryAdapter) fetchLatestNAVDiscovery(ctx context.Context) ([]domain.FundSeed, error) {
	endpoint, err := appendQuery("https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx", url.Values{
		"t":    {"8"},
		"page": {"1,50000"},
		"sort": {"fcode,asc"},
		"js":   {"reData"},
	})
	if err != nil {
		return nil, err
	}
	body, err := a.client.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return ParseFundDiscoveryFromLatestNAVPayload(body)
}

func (a *DiscoveryAdapter) fetchDiscoveryPage(ctx context.Context, source CategorySource, fs string) ([]domain.FundSeed, error) {
	var failures []string
	for _, baseURL := range a.client.quoteBaseURLs {
		clistURL := strings.Replace(baseURL, "/api/qt/ulist.np/get", "/api/qt/clist/get", 1)
		seeds, err := a.fetchDiscoveryPages(ctx, clistURL, source, fs)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		return seeds, nil
	}
	return nil, errors.New(strings.Join(failures, "; "))
}

func (a *DiscoveryAdapter) fetchDiscoveryPages(ctx context.Context, clistURL string, source CategorySource, fs string) ([]domain.FundSeed, error) {
	items := make(map[string]domain.FundSeed)
	fetched := 0
	for page := 1; ; page++ {
		endpoint, err := appendQuery(clistURL, url.Values{
			"pn":     {fmt.Sprintf("%d", page)},
			"pz":     {fmt.Sprintf("%d", discoveryPageSize)},
			"po":     {"1"},
			"np":     {"1"},
			"fltt":   {"2"},
			"invt":   {"2"},
			"wbp2u":  {"|0|0|0|web"},
			"fid":    {"f3"},
			"fs":     {fs},
			"fields": {"f12,f13,f14"},
			"ut":     {"bd1d9ddb04089700cf9c27f6f7426281"},
		})
		if err != nil {
			return nil, err
		}
		body, err := a.client.get(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		seeds, total, err := parseFundDiscoveryPayload(body, source)
		if err != nil {
			return nil, err
		}
		for _, seed := range seeds {
			items[seed.Code] = seed
		}
		fetched += len(seeds)
		if fetched >= total || len(seeds) == 0 {
			break
		}
	}

	seeds := make([]domain.FundSeed, 0, len(items))
	for _, seed := range items {
		seeds = append(seeds, seed)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Code < seeds[j].Code })
	if len(seeds) == 0 {
		return nil, errors.New("eastmoney: fund discovery data missing")
	}
	return seeds, nil
}

func ParseFundDiscoveryPayload(body []byte, source CategorySource) ([]domain.FundSeed, error) {
	seeds, _, err := parseFundDiscoveryPayload(body, source)
	return seeds, err
}

func parseFundDiscoveryPayload(body []byte, source CategorySource) ([]domain.FundSeed, int, error) {
	var payload discoveryPayload
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, 0, err
	}
	if payload.Code != 0 {
		return nil, 0, fmt.Errorf("eastmoney: fund discovery response code %d", payload.Code)
	}

	seeds := make([]domain.FundSeed, 0, len(payload.Data.Diff))
	for _, entry := range payload.Data.Diff {
		market := rawString(entry.Market)
		if entry.Code == "" || entry.Name == "" || market == "" || isTerminatedFundName(entry.Name) {
			continue
		}
		seeds = append(seeds, domain.FundSeed{
			Code:       entry.Code,
			Name:       entry.Name,
			Category:   sourceCategory(source),
			Exchange:   exchangeName(market),
			QuoteSecID: market + "." + entry.Code,
			NAVCode:    entry.Code,
			Enabled:    true,
		})
	}
	if len(seeds) == 0 {
		return nil, payload.Data.Total, errors.New("eastmoney: fund discovery data missing")
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Code < seeds[j].Code })
	return seeds, payload.Data.Total, nil
}

func isTerminatedFundName(name string) bool {
	for _, marker := range []string{"退市", "摘牌", "终止上市"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func sourceCategory(source CategorySource) domain.FundCategory {
	switch source {
	case CategorySourceLOF:
		return domain.CategoryOtherLOF
	default:
		return domain.CategoryETF
	}
}

func exchangeName(market string) string {
	switch market {
	case "0":
		return "SZ"
	case "1":
		return "SH"
	default:
		return market
	}
}
