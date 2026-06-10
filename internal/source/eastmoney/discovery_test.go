package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gogap/internal/domain"
)

const clistDiscoveryPayload = `{"rc":0,"data":{"total":2,"diff":[{"f12":"501018","f13":1,"f14":"南方原油LOF"},{"f12":"511880","f13":1,"f14":"银华日利ETF"}]}}`
const quotePayloadWithChange = `{"rc":0,"data":{"diff":[{"f2":1.362,"f3":0.74,"f6":34958553.13,"f8":2.15,"f12":"159357","f13":0,"f14":"A500ETF博时","f20":123000000,"f124":1780218600}]}}`
const fundListPayload = `var reData={datas:[["159357","A500ETF博时","","1.3340","1.3340","1.3172","1.3172","0.0168","1.28","开放申购","开放赎回","-","-","-","-","-","-","0.15%","-","-","-"],["501018","南方原油LOF","","1.2040","1.2040","1.2096","1.2096","-0.0056","-0.46","开放申购","开放赎回","-","-","-","-","-","-","0.15%","-","-","-"]],record:"2",pages:"1",curpage:"1",showday:["2026-05-29","2026-05-28"]}`
const akshareFundListPayload = `var db={datas:[["159357","A500ETF博时","","1.3340","1.3340","1.3172","1.3172","0.0168","1.28","开放申购","开放赎回","-","-","-","-","-","-","0.15%","-","-","-"]],record:"1",pages:"1",curpage:"1",showday:["2026-05-29","2026-05-28"]}`

func TestParseFundDiscoveryPayloadBuildsSeeds(t *testing.T) {
	seeds, err := ParseFundDiscoveryPayload([]byte(clistDiscoveryPayload), CategorySourceLOF)
	if err != nil {
		t.Fatalf("ParseFundDiscoveryPayload returned error: %v", err)
	}

	if len(seeds) != 2 {
		t.Fatalf("expected 2 discovered seeds, got %d", len(seeds))
	}

	first := seeds[0]
	if first.Code != "501018" || first.Name != "南方原油LOF" || first.QuoteSecID != "1.501018" || first.NAVCode != "501018" || !first.Enabled {
		t.Fatalf("unexpected first seed: %+v", first)
	}
	if first.Exchange != "SH" {
		t.Fatalf("expected market 1 to map to SH, got %q", first.Exchange)
	}
	if first.Category != domain.CategoryOtherLOF {
		t.Fatalf("expected LOF discovery rows to use source category before NAV type enrichment, got %q", first.Category)
	}

	second := seeds[1]
	if second.Category != domain.CategoryOtherLOF {
		t.Fatalf("expected LOF discovery rows not to classify from fund name keywords, got %q", second.Category)
	}
}

func TestParseFundDiscoveryPayloadUsesSourceCategoryInsteadOfNameKeywords(t *testing.T) {
	seeds, err := ParseFundDiscoveryPayload([]byte(`{"rc":0,"data":{"total":3,"diff":[{"f12":"159792","f13":0,"f14":"港股通互联网ETF富国"},{"f12":"513100","f13":1,"f14":"纳指ETF"},{"f12":"513180","f13":1,"f14":"恒生科技ETF"}]}}`), CategorySourceETF)
	if err != nil {
		t.Fatalf("ParseFundDiscoveryPayload returned error: %v", err)
	}
	for _, seed := range seeds {
		if seed.Category != domain.CategoryETF {
			t.Fatalf("expected ETF discovery row %s to use source category instead of name keywords, got %+v", seed.Code, seed)
		}
	}
}

func TestParseLatestNAVPayloadMapsReturnedFundTypeToCategory(t *testing.T) {
	navs, err := ParseLatestNAVPayload([]byte(`var reData={datas:[
		["159792","无关键词一号","QDII-ETF","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["501019","无关键词二号","指数型-股票","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["501097","无关键词三号","混合型-灵活","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["511880","无关键词四号","货币型","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["501025","无关键词六号","香港股票","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["513100","纳指ETF国泰","指数型-海外股票","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["159920","恒生ETF","指数型-海外股票","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["159934","黄金ETF","指数型-商品","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload returned error: %v", err)
	}
	want := map[string]domain.FundCategory{
		"159792": domain.CategoryQDII,
		"501019": domain.CategoryIndexLOF,
		"501097": domain.CategoryActiveLOF,
		"511880": domain.CategoryBondMoney,
		"501025": domain.CategoryHongKong,
		"513100": domain.CategoryQDII,
		"159920": domain.CategoryHongKong,
		"159934": domain.CategoryCommodity,
	}
	for code, category := range want {
		if navs[code].Category != category {
			t.Fatalf("NAV %s category = %q, want %q; nav=%+v", code, navs[code].Category, category, navs[code])
		}
	}
}

func TestParseMetadataPayloadClearsLimitWhenPurchasePaused(t *testing.T) {
	metadata, err := ParseMetadataPayload([]byte(`var reData={datas:[["160644","基金160644","混合型","1.0000","2026-05-29","暂停申购","开放赎回","","100","10000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseMetadataPayload returned error: %v", err)
	}
	if metadata["160644"].PurchaseLimit != "" {
		t.Fatalf("expected paused purchase fund limit to be cleared, got %+v", metadata["160644"])
	}
}

func TestParseQuotePayloadBuildsChangePercent(t *testing.T) {
	quotes, err := ParseQuotePayload([]byte(quotePayloadWithChange))
	if err != nil {
		t.Fatalf("ParseQuotePayload returned error: %v", err)
	}
	quote := quotes["0.159357"]
	if quote.ChangePercent == nil || *quote.ChangePercent != 0.74 {
		t.Fatalf("unexpected quote change percent: %+v", quote)
	}
	if quote.MarketValue == nil || *quote.MarketValue != 123000000 {
		t.Fatalf("unexpected quote market value: %+v", quote)
	}
	if quote.Name != "A500ETF博时" {
		t.Fatalf("expected exchange quote name to be preserved, got %+v", quote)
	}
}

func TestParseQuotePayloadSkipsMalformedQuoteRows(t *testing.T) {
	quotes, err := ParseQuotePayload([]byte(`{"rc":0,"data":{"diff":[{"f2":"100/99","f3":0.74,"f6":1000,"f8":0.1,"f12":"159001","f13":0,"f14":"坏价格","f124":1780218600},{"f2":100,"f3":0.5,"f6":2000,"f8":0.2,"f12":"511880","f13":1,"f14":"银华日利ETF","f124":1780218600}]}}`))
	if err != nil {
		t.Fatalf("ParseQuotePayload returned error: %v", err)
	}
	if _, ok := quotes["0.159001"]; ok {
		t.Fatalf("expected malformed 100/99 quote row to be skipped, got %+v", quotes["0.159001"])
	}
	if quotes["1.511880"].Price == nil || *quotes["1.511880"].Price != 100 {
		t.Fatalf("expected valid numeric price 100 to be preserved, got %+v", quotes["1.511880"])
	}
}

func TestParseQuotePayloadKeepsPreOpenDashRowsAsNonTradable(t *testing.T) {
	quotes, err := ParseQuotePayload([]byte(`{"rc":0,"data":{"diff":[{"f2":"-","f3":"-","f6":"-","f8":0.0,"f12":"510300","f13":1,"f14":"沪深300ETF华泰柏瑞","f20":137992236304,"f124":1780446606}]}}`))
	if err != nil {
		t.Fatalf("ParseQuotePayload returned error: %v", err)
	}
	quote, ok := quotes["1.510300"]
	if !ok {
		t.Fatalf("expected pre-open dash quote row to be preserved, got %+v", quotes)
	}
	if quote.Tradable || quote.Price != nil || quote.TurnoverAmount != nil || quote.ChangePercent != nil {
		t.Fatalf("expected pre-open dash quote to be non-tradable without numeric fields, got %+v", quote)
	}
	if quote.Name != "沪深300ETF华泰柏瑞" || quote.Time == "" {
		t.Fatalf("expected pre-open quote metadata to be preserved, got %+v", quote)
	}
}

func TestParseQuotePayloadRejectsAllMalformedQuoteRows(t *testing.T) {
	_, err := ParseQuotePayload([]byte(`{"rc":0,"data":{"diff":[{"f2":"100/99","f3":0.74,"f6":1000,"f8":0.1,"f12":"159001","f13":0,"f14":"坏价格","f124":1780218600}]}}`))
	if err == nil {
		t.Fatal("expected all-malformed quote payload to return error")
	}
}

func TestParseQuotePayloadSkipsAbnormalUntradedFundRows(t *testing.T) {
	quotes, err := ParseQuotePayload([]byte(`{"rc":0,"data":{"diff":[{"f2":100,"f3":0,"f6":0,"f8":"-","f12":"159001","f13":0,"f14":"保证金ETF","f124":0},{"f2":1.001,"f3":0.1,"f6":2000,"f8":0.2,"f12":"159915","f13":0,"f14":"创业板ETF","f124":1780218600},{"f2":100,"f3":0.5,"f6":2000,"f8":0.2,"f12":"511880","f13":1,"f14":"银华日利ETF","f124":1780218600}]}}`))
	if err != nil {
		t.Fatalf("ParseQuotePayload returned error: %v", err)
	}
	if _, ok := quotes["0.159001"]; ok {
		t.Fatalf("expected abnormal untraded 159001 row to be skipped, got %+v", quotes["0.159001"])
	}
	if quotes["0.159915"].Price == nil || *quotes["0.159915"].Price != 1.001 {
		t.Fatalf("expected valid ETF row to be preserved, got %+v", quotes["0.159915"])
	}
	if quotes["1.511880"].Price == nil || *quotes["1.511880"].Price != 100 {
		t.Fatalf("expected valid money fund quote at 100 to be preserved, got %+v", quotes["1.511880"])
	}
}

func TestParseFundDiscoveryPayloadRejectsEmptyList(t *testing.T) {
	_, err := ParseFundDiscoveryPayload([]byte(`{"rc":0,"data":{"total":0,"diff":[]}}`), CategorySourceETF)
	if err == nil {
		t.Fatal("expected empty discovery payload to fail")
	}
}

func TestParseFundDiscoveryPayloadSkipsTerminatedEntries(t *testing.T) {
	seeds, err := ParseFundDiscoveryPayload([]byte(`{"rc":0,"data":{"total":2,"diff":[{"f12":"501092","f13":1,"f14":"互联互通LOF退市"},{"f12":"501018","f13":1,"f14":"南方原油LOF"}]}}`), CategorySourceLOF)
	if err != nil {
		t.Fatalf("ParseFundDiscoveryPayload returned error: %v", err)
	}
	if len(seeds) != 1 || seeds[0].Code != "501018" {
		t.Fatalf("expected terminated 501092 row to be skipped, got %+v", seeds)
	}
}

func TestParseFundDiscoveryPayloadKeepsREITNamedLOFEntries(t *testing.T) {
	seeds, err := ParseFundDiscoveryPayload([]byte(`{"rc":0,"data":{"total":2,"diff":[{"f12":"160140","f13":0,"f14":"美国REIT精选LOF"},{"f12":"501018","f13":1,"f14":"南方原油LOF"}]}}`), CategorySourceLOF)
	if err != nil {
		t.Fatalf("ParseFundDiscoveryPayload returned error: %v", err)
	}
	if len(seeds) != 2 || seeds[0].Code != "160140" || seeds[0].Category != domain.CategoryOtherLOF {
		t.Fatalf("expected REIT-named LOF to stay in the LOF pool, got %+v", seeds)
	}
}

func TestParseFundDiscoveryFromLatestNAVIncludesListedQDII(t *testing.T) {
	seeds, err := ParseFundDiscoveryFromLatestNAVPayload([]byte(`var reData={datas:[
		["164824","工银印度基金人民币","QDII-混合偏股","1.2957","05-29","限大额","开放赎回","","10.0","500000.0"],
		["000001","华夏成长混合","混合型-灵活","1.0000","05-29","开放申购","开放赎回","","10.0","100000000000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseFundDiscoveryFromLatestNAVPayload returned error: %v", err)
	}
	if len(seeds) != 1 || seeds[0].Code != "164824" || seeds[0].QuoteSecID != "0.164824" || seeds[0].Category != domain.CategoryQDII {
		t.Fatalf("expected listed QDII 164824 to be discovered from latest NAV, got %+v", seeds)
	}
}

func TestParseMetadataPayloadFormatsLargePurchaseLimit(t *testing.T) {
	metadata, err := ParseMetadataPayload([]byte(`var reData={datas:[["164824","工银印度基金人民币","QDII-混合偏股","1.2957","05-29","限大额","开放赎回","","10.0","500000.0"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseMetadataPayload returned error: %v", err)
	}
	if metadata["164824"].PurchaseLimit != "50万" {
		t.Fatalf("expected 164824 purchase limit to be formatted, got %+v", metadata["164824"])
	}
}

func TestParseLatestNAVPayloadExcludesREITType(t *testing.T) {
	navs, err := ParseLatestNAVPayload([]byte(`var reData={datas:[
		["160140","美国REIT精选LOF","QDII","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"],
		["508097","国泰君安东久新经济REIT","REITs","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload returned error: %v", err)
	}
	if _, ok := navs["508097"]; ok {
		t.Fatalf("expected REIT type row to be excluded, got %+v", navs["508097"])
	}
	if navs["160140"].Category != domain.CategoryQDII {
		t.Fatalf("expected REIT-named non-REIT type to remain, got %+v", navs["160140"])
	}
}

func TestParseLatestNAVPayloadBuildsNAVs(t *testing.T) {
	navs, err := ParseLatestNAVPayload([]byte(fundListPayload))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload returned error: %v", err)
	}
	first := navs["159357"]
	if first.Code != "159357" || first.NAV == nil || *first.NAV != 1.3340 || first.NAVDate != "2026-05-29" || first.ChangePercent == nil || *first.ChangePercent != 1.28 {
		t.Fatalf("unexpected 159357 NAV: %+v", first)
	}
	second := navs["501018"]
	if second.Code != "501018" || second.NAV == nil || *second.NAV != 1.2040 || second.NAVDate != "2026-05-29" {
		t.Fatalf("unexpected 501018 NAV: %+v", second)
	}
}

func TestParseLatestNAVPayloadAcceptsAKShareDBWrapper(t *testing.T) {
	navs, err := ParseLatestNAVPayload([]byte(akshareFundListPayload))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload returned error for AKShare db wrapper: %v", err)
	}
	if navs["159357"].NAVDate != "2026-05-29" {
		t.Fatalf("unexpected AKShare db wrapper NAV: %+v", navs["159357"])
	}
}

func TestParseLatestNAVPayloadUsesT8RowLayoutWhenAvailable(t *testing.T) {
	navs, err := ParseLatestNAVPayload([]byte(`var reData={datas:[["510300","沪深300ETF","指数型-股票","4.0000","2026-05-29","限大额","开放赎回","","100","1000000","","","0.15%"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload returned error: %v", err)
	}
	if navs["510300"].NAV == nil || *navs["510300"].NAV != 4.0 || navs["510300"].NAVDate != "2026-05-29" {
		t.Fatalf("unexpected t=8 row NAV: %+v", navs["510300"])
	}
	if navs["510300"].ChangePercent != nil {
		t.Fatalf("expected t=8 row without change percent to leave ChangePercent nil, got %+v", navs["510300"])
	}
}

func TestLatestNAVParamsMatchAKShareOpenFundDaily(t *testing.T) {
	params := latestNAVParams()
	for key, want := range map[string]string{
		"t":    "8",
		"sort": "fcode,asc",
		"page": "1,50000",
	} {
		if got := params.Get(key); got != want {
			t.Fatalf("latest NAV param %s = %q, want %q", key, got, want)
		}
	}
}

func TestNAVAdapterMergesT1ChangePercentIntoT8NAVRows(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		switch r.URL.Query().Get("t") {
		case "8":
			_, _ = w.Write([]byte(`var reData={datas:[["510300","沪深300ETF","指数型-股票","4.0000","2026-05-29","场内交易","场内交易","","100","1000000","","","0.15%"]],showday:["2026-05-29"]}`))
		case "1":
			_, _ = w.Write([]byte(`var db={datas:[["510300","沪深300ETF","","4.0000","4.0000","3.9000","3.9000","0.1000","2.56","场内交易","场内交易","-","-","-","-","-","-","0.15%","-","-","-"]],showday:["2026-05-29","2026-05-28"]}`))
		default:
			t.Fatalf("unexpected NAV query %s", r.URL.RawQuery)
		}
	}))
	t.Cleanup(server.Close)

	adapter := NewNAVAdapter(NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"}))
	navs, err := adapter.fetchNAVs(context.Background(), []string{"510300"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected t=8 and t=1 NAV requests, got %v", requests)
	}
	if navs["510300"].ChangePercent == nil || *navs["510300"].ChangePercent != 2.56 {
		t.Fatalf("expected t=1 change percent merged into t=8 NAV, got %+v", navs["510300"])
	}
}

func TestDiscoveryPoolUsesReturnedList(t *testing.T) {
	pool := NewDiscoveryPoolProvider(discoveryFetcherFunc(func(context.Context) ([]domain.FundSeed, error) {
		return []domain.FundSeed{{Code: "501018", Name: "南方原油LOF", QuoteSecID: "1.501018", NAVCode: "501018", Category: domain.CategoryCommodity, Exchange: "SH", Enabled: true}}, nil
	}))

	seeds, err := pool.FundPool(context.Background())
	if err != nil {
		t.Fatalf("FundPool returned error: %v", err)
	}
	if len(seeds) != 1 || seeds[0].Code != "501018" {
		t.Fatalf("expected returned discovery list to be primary, got %+v", seeds)
	}
}

func TestDiscoveryAdapterFetchesEveryDiscoveryPage(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		key := query.Get("fs") + ":" + query.Get("pn")
		requests[key]++
		if query.Get("pz") != "100" {
			t.Fatalf("expected AKShare discovery page size 100, got %q", query.Get("pz"))
		}
		switch key {
		case lofDiscoveryFS + ":1":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"total":3,"diff":[{"f12":"160106","f13":0,"f14":"南方高增LOF"},{"f12":"161127","f13":0,"f14":"标普生物科技LOF"}]}}`))
		case lofDiscoveryFS + ":2":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"total":3,"diff":[{"f12":"501018","f13":1,"f14":"南方原油LOF"}]}}`))
		case etfDiscoveryFS + ":1":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"total":2,"diff":[{"f12":"159357","f13":0,"f14":"A500ETF博时"},{"f12":"513100","f13":1,"f14":"纳指ETF"}]}}`))
		default:
			t.Fatalf("unexpected discovery request %s", key)
		}
		if query.Get("ut") != "bd1d9ddb04089700cf9c27f6f7426281" || query.Get("wbp2u") != "|0|0|0|web" || query.Get("fid") != "f3" {
			t.Fatalf("expected AKShare-style discovery params, got %s", r.URL.RawQuery)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), QuoteBaseURLs: []string{server.URL + "/api/qt/ulist.np/get"}})
	seeds, err := NewDiscoveryAdapter(client).FetchFundPool(context.Background())
	if err != nil {
		t.Fatalf("FetchFundPool returned error: %v", err)
	}

	if len(seeds) != 5 {
		t.Fatalf("expected all 5 non-REIT paginated seeds, got %d: %+v", len(seeds), seeds)
	}
	if requests[lofDiscoveryFS+":2"] != 1 {
		t.Fatalf("expected second LOF page to be fetched, requests=%v", requests)
	}
	if !hasCategory(seeds, domain.CategoryETF) {
		t.Fatalf("expected ETF source rows to remain included as ETF before NAV enrichment, got %+v", seeds)
	}
}

func TestDiscoveryAdapterPreservesETFSourceCategoryWhenLatestNAVDiscoveryOverlaps(t *testing.T) {
	seeds := mergeDiscoveredSeeds([]domain.FundSeed{
		{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
		{Code: "501019", Name: "军工LOF", Category: domain.CategoryOtherLOF, QuoteSecID: "1.501019", NAVCode: "501019", Enabled: true},
	}, []domain.FundSeed{
		{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
	})
	for _, seed := range seeds {
		if seed.Code == "510300" && seed.Category != domain.CategoryETF {
			t.Fatalf("expected overlapping latest NAV discovery not to overwrite ETF source category, got %+v", seed)
		}
	}
}

func TestDiscoveryAdapterStopsWhenFirstPageIsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := url.ParseQuery(r.URL.RawQuery)
		if query.Get("pn") != "1" {
			t.Fatalf("expected no extra page when total fits first page, got pn=%s", query.Get("pn"))
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"total":1,"diff":[{"f12":"159357","f13":0,"f14":"A500ETF博时"}]}}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), QuoteBaseURLs: []string{server.URL + "/api/qt/ulist.np/get"}})
	seeds, err := NewDiscoveryAdapter(client).FetchFundPool(context.Background())
	if err != nil {
		t.Fatalf("FetchFundPool returned error: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("expected one deduplicated seed, got %+v", seeds)
	}
}

func hasCategory(seeds []domain.FundSeed, category domain.FundCategory) bool {
	for _, seed := range seeds {
		if seed.Category == category {
			return true
		}
	}
	return false
}

type discoveryFetcherFunc func(context.Context) ([]domain.FundSeed, error)

func (f discoveryFetcherFunc) FetchFundPool(ctx context.Context) ([]domain.FundSeed, error) {
	return f(ctx)
}
