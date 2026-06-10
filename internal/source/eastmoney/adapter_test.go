package eastmoney

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetadataAdapterFillsScaleFromOverviewForTrackedLists(t *testing.T) {
	overviewRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["000001","基金1","混合型","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000","","","0.15%"]],showday:["2026-05-29"]}`))
			return
		}
		overviewRequests++
		_, _ = w.Write([]byte(`净资产规模：1,999.14亿元（截止至：2026年05月29日）`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx", MinInterval: time.Nanosecond})
	adapter := NewMetadataAdapter(client)
	oldInterval := overviewRequestInterval
	overviewRequestInterval = 0
	t.Cleanup(func() { overviewRequestInterval = oldInterval })
	codes := []string{"000001", "000002", "000003", "000004", "000005", "000006", "000007", "000008", "000009", "000010", "000011"}
	results, err := adapter.fetchMetadata(context.Background(), codes, server.URL+"/Data/Fund_JJJZ_Data.aspx", server.URL+"/jbgk_", nil)
	if err != nil {
		t.Fatalf("fetchMetadata returned error: %v", err)
	}
	if overviewRequests != len(codes) {
		t.Fatalf("expected large metadata refresh to fetch overview scale for every code, got %d", overviewRequests)
	}
	if results["000001"].PurchaseLimit != "" {
		t.Fatalf("expected overview without limit text not to preserve stale bulk purchase limit, got %+v", results["000001"])
	}
	if results["000001"].FundScale != "1,999.14亿元" {
		t.Fatalf("expected overview fund scale to be preserved, got %+v", results["000001"])
	}
}

func TestMetadataAdapterSkipsOverviewForFullMarketLists(t *testing.T) {
	overviewRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["000001","基金1","混合型","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000","","","0.15%"]],showday:["2026-05-29"]}`))
			return
		}
		overviewRequests++
		_, _ = w.Write([]byte(`净资产规模：1,999.14亿元（截止至：2026年05月29日）`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx", MinInterval: time.Nanosecond})
	adapter := NewMetadataAdapter(client)
	codes := make([]string, 201)
	for index := range codes {
		codes[index] = "00" + strings.Repeat("0", 4-len(string(rune('0'+index%10)))) + string(rune('0'+index%10))
	}
	results, err := adapter.fetchMetadata(context.Background(), codes, server.URL+"/Data/Fund_JJJZ_Data.aspx", server.URL+"/jbgk_", nil)
	if err != nil {
		t.Fatalf("fetchMetadata returned error: %v", err)
	}
	if overviewRequests != 0 {
		t.Fatalf("expected full market metadata refresh to avoid per-fund overview requests, got %d", overviewRequests)
	}
	if results["000001"].PurchaseLimit != "100万" {
		t.Fatalf("expected bulk purchase limit to be preserved for full market refresh without overview, got %+v", results["000001"])
	}
}

func TestNAVAdapterFallsBackForCodesMissingFromLatestBatch(t *testing.T) {
	fallbackRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["000001","基金1","","1.0000","1.0000","1.0000","1.0000","0.0000","0.00"]],showday:["2026-05-29"]}`))
			return
		}
		fallbackRequests++
		if r.URL.Query().Get("code") != "000002" {
			t.Fatalf("expected fallback request for missing code 000002, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`2026-05-29 单位净值 2.0000`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	results, err := adapter.fetchNAVs(context.Background(), []string{"000001", "000002"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if fallbackRequests != 1 {
		t.Fatalf("expected one per-code fallback request, got %d", fallbackRequests)
	}
	if results["000001"].NAV == nil || *results["000001"].NAV != 1.0 {
		t.Fatalf("expected bulk NAV for 000001, got %+v", results["000001"])
	}
	if results["000002"].NAV == nil || *results["000002"].NAV != 2.0 {
		t.Fatalf("expected fallback NAV for 000002, got %+v", results["000002"])
	}
}

func TestParseNAVPayloadReadsDailyGrowthFromF10History(t *testing.T) {
	result, err := ParseNAVPayload("159007", []byte(`var apidata={ content:"<table><tbody><tr><td>2026-06-01</td><td class='tor bold'>0.8825</td><td class='tor bold'>0.8825</td><td class='tor bold red'>1.29%</td><td>场内买入</td><td>场内卖出</td><td></td></tr></tbody></table>",records:32,pages:32,curpage:1};`))
	if err != nil {
		t.Fatalf("ParseNAVPayload returned error: %v", err)
	}
	if result.NAV == nil || *result.NAV != 0.8825 || result.NAVDate != "2026-06-01" {
		t.Fatalf("expected F10 NAV date/value, got %+v", result)
	}
	if result.ChangePercent == nil || *result.ChangePercent != 1.29 {
		t.Fatalf("expected F10 daily growth percent, got %+v", result)
	}
}

func TestNAVAdapterFillsF10GrowthForMissingBulkChangePercent(t *testing.T) {
	f10Requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			if r.URL.Query().Get("t") == "1" {
				_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01","2026-05-29"]}`))
				return
			}
			_, _ = w.Write([]byte(`var reData={datas:[["159007","养殖ETF华泰柏瑞","指数型-股票","0.8825","06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01","2026-05-29"]}`))
			return
		}
		f10Requests++
		if r.URL.Query().Get("code") != "159007" {
			t.Fatalf("expected F10 request for 159007, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`var apidata={ content:"<table><tbody><tr><td>2026-06-01</td><td class='tor bold'>0.8825</td><td class='tor bold'>0.8825</td><td class='tor bold red'>1.29%</td><td>开放申购</td><td>开放赎回</td><td></td></tr></tbody></table>",records:32,pages:32,curpage:1};`))
	}))
	t.Cleanup(server.Close)
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	t.Cleanup(func() { requestPacingDelay = oldDelay })

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	results, err := adapter.fetchNAVs(context.Background(), []string{"159007"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if results["159007"].ChangePercent != nil {
		t.Fatalf("expected bulk NAV fetch to leave missing change percent alone, got %+v", results["159007"])
	}
	results, err = adapter.FillMissingChangePercent(context.Background(), results, []string{"159007"})
	if err != nil {
		t.Fatalf("FillMissingChangePercent returned error: %v", err)
	}
	if f10Requests != 1 {
		t.Fatalf("expected one F10 growth fallback request, got %d", f10Requests)
	}
	if results["159007"].NAV == nil || *results["159007"].NAV != 0.8825 {
		t.Fatalf("expected t=8 NAV preserved, got %+v", results["159007"])
	}
	if results["159007"].ChangePercent == nil || *results["159007"].ChangePercent != 1.29 {
		t.Fatalf("expected F10 daily growth merged, got %+v", results["159007"])
	}
}

func TestNAVAdapterFillsListedGrowthBeforeF10Fallback(t *testing.T) {
	f10Requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			if r.URL.Query().Get("t") == "1" {
				_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01","2026-05-29"]}`))
				return
			}
			_, _ = w.Write([]byte(`var reData={datas:[["159934","黄金ETF易方达","指数型-其他","9.7624","06-01","场内交易","场内交易","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01","2026-05-29"]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/cnjy_dwjz.html") {
			_, _ = w.Write([]byte(`<table><tr><td>159934</td><td>黄金ETF易方达</td><td>指数型-其他</td><td>9.7624</td><td>2.3509</td><td>-0.0474</td><td>-0.48%</td><td>9.7780</td><td>0.07%</td></tr></table>`))
			return
		}
		f10Requests++
		_, _ = w.Write([]byte(`var apidata={ content:"暂无数据",records:0,pages:0,curpage:1};`))
	}))
	t.Cleanup(server.Close)
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	t.Cleanup(func() { requestPacingDelay = oldDelay })

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	adapter.listedNAVURL = server.URL + "/cnjy_dwjz.html"
	results, err := adapter.fetchNAVs(context.Background(), []string{"159934"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	results, err = adapter.FillMissingChangePercent(context.Background(), results, []string{"159934"})
	if err != nil {
		t.Fatalf("FillMissingChangePercent returned error: %v", err)
	}
	if f10Requests != 0 {
		t.Fatalf("expected listed batch growth to avoid F10 requests, got %d", f10Requests)
	}
	if results["159934"].ChangePercent == nil || *results["159934"].ChangePercent != -0.48 {
		t.Fatalf("expected listed batch growth to fill change percent, got %+v", results["159934"])
	}
}

func TestNAVAdapterKeepsBulkResultsWhenMissingCodeFallbackIsMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["000001","基金1","混合型","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"]],showday:["2026-05-29"]}`))
			return
		}
		_, _ = w.Write([]byte(`var apidata={ content:"暂无数据",records:0,pages:0,curpage:1};`))
	}))
	t.Cleanup(server.Close)
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	t.Cleanup(func() { requestPacingDelay = oldDelay })

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	results, err := adapter.fetchNAVs(context.Background(), []string{"000001", "000002"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs should keep valid bulk rows when a missing-code fallback is malformed, got error: %v", err)
	}
	if len(results) != 1 || results["000001"].NAV == nil || *results["000001"].NAV != 1.0 {
		t.Fatalf("expected valid bulk row to remain, got %+v", results)
	}
}

func TestMergeNAVChangePercentFillsFromT1WithoutOverwritingNAV(t *testing.T) {
	t8, err := ParseLatestNAVPayload([]byte(`var reData={datas:[["000001","基金1","混合型","1.0000","2026-05-29","开放申购","开放赎回","","100","1000000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload t=8 returned error: %v", err)
	}
	t1, err := ParseLatestNAVPayload([]byte(`var db={datas:[["000001","基金1","混合型","1.0100","2026-05-29","开放申购","开放赎回","0.0200","1.98","1000000"]],showday:["2026-05-29"]}`))
	if err != nil {
		t.Fatalf("ParseLatestNAVPayload t=1 returned error: %v", err)
	}

	merged := mergeNAVChangePercent(t8["000001"], t1["000001"])
	if merged.NAV == nil || *merged.NAV != 1.0 {
		t.Fatalf("expected t=8 NAV value to be preserved, got %+v", merged)
	}
	if merged.ChangePercent == nil || *merged.ChangePercent != 1.98 {
		t.Fatalf("expected t=1 NAV change percent to be merged, got %+v", merged)
	}
}

func TestParseEstimatedNAVPayloadReadsFundGZEstimate(t *testing.T) {
	results, err := ParseEstimatedNAVPayload([]byte(`jsonpgz({"fundcode":"510300","name":"沪深300ETF华泰柏瑞","jzrq":"2026-06-01","dwjz":"4.8672","gsz":"4.9378","gszzl":"1.45","gztime":"2026-06-02 15:00"});`))
	if err != nil {
		t.Fatalf("ParseEstimatedNAVPayload returned error: %v", err)
	}
	result := results["510300"]
	if result.EstimatedNAV == nil || *result.EstimatedNAV != 4.9378 || result.EstimatedNAVTime != "2026-06-02 15:00" {
		t.Fatalf("expected estimated NAV and time, got %+v", result)
	}
	if result.EstimatedChangePercent == nil || *result.EstimatedChangePercent != 1.45 {
		t.Fatalf("expected estimated change percent, got %+v", result)
	}
}

func TestParseEstimatedNAVPayloadIgnoresEmptyEstimate(t *testing.T) {
	results, err := ParseEstimatedNAVPayload([]byte(`jsonpgz();`))
	if err != nil {
		t.Fatalf("ParseEstimatedNAVPayload returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty estimate payload to return no results, got %+v", results)
	}
}

func TestNAVAdapterMergesEstimatedNAVWithoutOverwritingOfficialNAV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["510300","沪深300ETF华泰柏瑞","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01"]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/js/510300.js") {
			_, _ = w.Write([]byte(`jsonpgz({"fundcode":"510300","name":"沪深300ETF华泰柏瑞","jzrq":"2026-06-01","dwjz":"4.8672","gsz":"4.9378","gszzl":"1.45","gztime":"2026-06-02 15:00"});`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/510300.js"
	results, err := adapter.fetchNAVs(context.Background(), []string{"510300"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	result := results["510300"]
	if result.NAV == nil || *result.NAV != 4.8672 || result.NAVDate != "2026-06-01" {
		t.Fatalf("expected official NAV preserved, got %+v", result)
	}
	if result.EstimatedNAV == nil || *result.EstimatedNAV != 4.9378 || result.EstimatedNAVTime != "2026-06-02 15:00" {
		t.Fatalf("expected estimate merged, got %+v", result)
	}
}

func TestNAVAdapterDefaultEstimateURLUsesFundGZJSONPPath(t *testing.T) {
	requestedEstimate := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["510300","沪深300ETF华泰柏瑞","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01"]}`))
			return
		}
		if r.URL.Path == "/js/510300.js" {
			requestedEstimate = true
			_, _ = w.Write([]byte(`jsonpgz({"fundcode":"510300","name":"沪深300ETF华泰柏瑞","jzrq":"2026-06-01","dwjz":"4.8672","gsz":"4.9378","gszzl":"1.45","gztime":"2026-06-02 15:00"});`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/"
	results, err := adapter.fetchNAVs(context.Background(), []string{"510300"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if !requestedEstimate {
		t.Fatalf("expected fundgz-style /js/{code}.js estimate request")
	}
	if results["510300"].EstimatedNAV == nil || *results["510300"].EstimatedNAV != 4.9378 {
		t.Fatalf("expected fundgz estimate merged, got %+v", results["510300"])
	}
}

func TestNAVAdapterLimitsEstimateRequestsDuringStartup(t *testing.T) {
	estimateRequests := int32(0)
	codes := make([]string, 0, 202)
	codes = append(codes, "510300", "159582")
	for i := 0; i < 200; i++ {
		codes = append(codes, fmt.Sprintf("9%05d", i))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			rows := make([]string, 0, len(codes))
			for _, code := range codes {
				rows = append(rows, fmt.Sprintf(`["%s","基金%s","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%%"]`, code, code))
			}
			_, _ = w.Write([]byte(`var reData={datas:[` + strings.Join(rows, ",") + `],showday:["2026-06-01"]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/js/") {
			atomic.AddInt32(&estimateRequests, 1)
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte(`jsonpgz();`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx"})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/"
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	t.Cleanup(func() { requestPacingDelay = oldDelay })
	started := time.Now()
	_, err := adapter.fetchNAVs(context.Background(), codes, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("expected estimate fetches not to serialize startup, elapsed %s", elapsed)
	}
	if got := atomic.LoadInt32(&estimateRequests); got != int32(len(codes)) {
		t.Fatalf("expected estimate requests for official NAV results, got %d", got)
	}
}

func TestNAVAdapterDoesNotBlockOfficialNAVOnSlowEstimates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["510300","沪深300ETF华泰柏瑞","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01"]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/js/") {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`jsonpgz({"fundcode":"510300","name":"沪深300ETF华泰柏瑞","jzrq":"2026-06-01","dwjz":"4.8672","gsz":"4.9378","gszzl":"1.45","gztime":"2026-06-02 15:00"});`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx", MinInterval: time.Nanosecond})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/"
	oldTimeout := estimateFetchTimeout
	estimateFetchTimeout = 20 * time.Millisecond
	t.Cleanup(func() { estimateFetchTimeout = oldTimeout })
	started := time.Now()
	results, err := adapter.fetchNAVs(context.Background(), []string{"510300"}, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("expected slow estimates not to block official NAV, elapsed %s", elapsed)
	}
	if results["510300"].NAV == nil || *results["510300"].NAV != 4.8672 {
		t.Fatalf("expected official NAV returned despite slow estimate, got %+v", results["510300"])
	}
}

func TestNAVAdapterSkipsPerCodeFallbackForLargeStartupUniverse(t *testing.T) {
	fallbackRequests := int32(0)
	codes := make([]string, 0, 202)
	codes = append(codes, "510300", "159582")
	for i := 0; i < 200; i++ {
		codes = append(codes, fmt.Sprintf("9%05d", i))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			_, _ = w.Write([]byte(`var reData={datas:[["510300","沪深300ETF华泰柏瑞","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"],["159582","半导体ETF博时","指数型-股票","3.2068","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%"]],showday:["2026-06-01"]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/F10DataApi.aspx") {
			atomic.AddInt32(&fallbackRequests, 1)
			_, _ = w.Write([]byte(`var apidata={content:"",records:0,pages:0,curpage:1}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/js/") {
			_, _ = w.Write([]byte(`jsonpgz();`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx", MinInterval: time.Nanosecond})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/"
	oldTimeout := estimateFetchTimeout
	estimateFetchTimeout = 20 * time.Millisecond
	t.Cleanup(func() { estimateFetchTimeout = oldTimeout })
	results, err := adapter.fetchNAVs(context.Background(), codes, server.URL+"/Data/Fund_JJJZ_Data.aspx")
	if err != nil {
		t.Fatalf("fetchNAVs returned error: %v", err)
	}
	if got := atomic.LoadInt32(&fallbackRequests); got != 0 {
		t.Fatalf("expected no per-code fallback requests for large startup universe, got %d", got)
	}
	if len(results) != 2 {
		t.Fatalf("expected latest NAV results to be preserved, got %d", len(results))
	}
}

func TestDefaultEstimateFetchTimeoutKeepsStartupBounded(t *testing.T) {
	if estimateFetchTimeout > 3*time.Second {
		t.Fatalf("estimate fetch timeout = %s, want <= 3s", estimateFetchTimeout)
	}
}

func TestQuoteAdapterUsesLargeStartupBatches(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		secIDs := strings.Split(r.URL.Query().Get("secids"), ",")
		entries := make([]string, 0, len(secIDs))
		for _, secID := range secIDs {
			parts := strings.Split(secID, ".")
			if len(parts) != 2 {
				t.Fatalf("unexpected secid %q", secID)
			}
			entries = append(entries, `{"f2":1.23,"f3":0.5,"f6":1000,"f8":0.1,"f12":"`+parts[1]+`","f13":`+parts[0]+`,"f14":"基金","f124":1780293600}`)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[` + strings.Join(entries, ",") + `]}}`))
	}))
	t.Cleanup(server.Close)

	secIDs := make([]string, 121)
	for index := range secIDs {
		secIDs[index] = "1." + strings.Repeat("0", 6-len(string(rune('0'+index%10))))
		secIDs[index] = "1.5" + strings.Repeat("0", 4) + string(rune('0'+index%10))
	}
	client := NewClient(Options{HTTPClient: server.Client(), QuoteBaseURLs: []string{server.URL + "/api/qt/ulist.np/get"}})
	adapter := NewQuoteAdapter(client)
	results, err := adapter.FetchQuotes(context.Background(), secIDs)
	if err != nil {
		t.Fatalf("FetchQuotes returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected quote results")
	}
	if requests > 3 {
		t.Fatalf("expected at most 3 quote batch requests for 121 secids, got %d", requests)
	}
}

func TestQuoteAdapterFetchQuotesDoesNotSerializeBatchesOnOuterPacing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secIDs := strings.Split(r.URL.Query().Get("secids"), ",")
		entries := make([]string, 0, len(secIDs))
		for _, secID := range secIDs {
			parts := strings.Split(secID, ".")
			if len(parts) != 2 {
				t.Fatalf("unexpected secid %q", secID)
			}
			entries = append(entries, `{"f2":1.23,"f3":0.5,"f6":1000,"f8":0.1,"f12":"`+parts[1]+`","f13":`+parts[0]+`,"f14":"基金","f124":1780293600}`)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[` + strings.Join(entries, ",") + `]}}`))
	}))
	t.Cleanup(server.Close)

	secIDs := make([]string, quoteBatchSize*3)
	for index := range secIDs {
		secIDs[index] = fmt.Sprintf("1.%06d", 510000+index)
	}
	client := NewClient(Options{HTTPClient: server.Client(), QuoteBaseURLs: []string{server.URL + "/api/qt/ulist.np/get"}, MinInterval: time.Nanosecond})
	adapter := NewQuoteAdapter(client)
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 150 * time.Millisecond }
	t.Cleanup(func() { requestPacingDelay = oldDelay })

	started := time.Now()
	results, err := adapter.FetchQuotes(context.Background(), secIDs)
	if err != nil {
		t.Fatalf("FetchQuotes returned error: %v", err)
	}
	if len(results) != len(secIDs) {
		t.Fatalf("FetchQuotes returned %d rows, want %d", len(results), len(secIDs))
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("expected quote batches not to serialize on outer pacing, elapsed %s", elapsed)
	}
}

func TestWaitBetweenRequestsUsesAKShareDelay(t *testing.T) {
	oldDelay := requestPacingDelay
	called := false
	requestPacingDelay = func() time.Duration {
		called = true
		return 0
	}
	t.Cleanup(func() { requestPacingDelay = oldDelay })

	if err := waitBetweenRequests(context.Background(), true); err != nil {
		t.Fatalf("waitBetweenRequests returned error: %v", err)
	}
	if !called {
		t.Fatal("expected waitBetweenRequests to use AKShare pacing delay")
	}
}

func TestDefaultRequestPacingDelayMatchesAKShareJitterBounds(t *testing.T) {
	delays := map[time.Duration]bool{}
	for range 64 {
		delay := requestPacingDelay()
		if delay < akShareRequestDelayMin || delay > akShareRequestDelayMax {
			t.Fatalf("requestPacingDelay() = %v, want within [%v, %v]", delay, akShareRequestDelayMin, akShareRequestDelayMax)
		}
		delays[delay] = true
	}
	if len(delays) == 1 {
		t.Fatalf("expected AKShare-style jittered pacing, got one fixed delay %v", maps.Keys(delays))
	}
}

func BenchmarkQuoteAdapterFetchQuotesLargeUniverse(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secIDs := strings.Split(r.URL.Query().Get("secids"), ",")
		entries := make([]string, 0, len(secIDs))
		for _, secID := range secIDs {
			parts := strings.Split(secID, ".")
			entries = append(entries, `{"f2":1.23,"f3":0.5,"f6":1000,"f8":0.1,"f12":"`+parts[1]+`","f13":`+parts[0]+`,"f14":"基金","f124":1780293600}`)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[` + strings.Join(entries, ",") + `]}}`))
	}))
	b.Cleanup(server.Close)

	secIDs := make([]string, 600)
	for index := range secIDs {
		secIDs[index] = fmt.Sprintf("1.%06d", 500000+index)
	}
	client := NewClient(Options{HTTPClient: server.Client(), QuoteBaseURLs: []string{server.URL + "/api/qt/ulist.np/get"}, MinInterval: time.Nanosecond})
	adapter := NewQuoteAdapter(client)
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	b.Cleanup(func() { requestPacingDelay = oldDelay })

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		results, err := adapter.FetchQuotes(context.Background(), secIDs)
		if err != nil {
			b.Fatalf("FetchQuotes returned error: %v", err)
		}
		if len(results) != len(secIDs) {
			b.Fatalf("FetchQuotes returned %d rows, want %d", len(results), len(secIDs))
		}
	}
}

func BenchmarkNAVAdapterFetchNAVsLargeUniverseWithEstimates(b *testing.B) {
	codes := make([]string, 360)
	for index := range codes {
		codes[index] = fmt.Sprintf("%06d", 510000+index)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/Data/Fund_JJJZ_Data.aspx") {
			rows := make([]string, 0, len(codes))
			for _, code := range codes {
				rows = append(rows, fmt.Sprintf(`["%s","基金%s","指数型-股票","4.8672","2026-06-01","开放申购","开放赎回","","10.0","100000000000","1.0","1","0.15%%"]`, code, code))
			}
			_, _ = w.Write([]byte(`var reData={datas:[` + strings.Join(rows, ",") + `],showday:["2026-06-01"]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/js/") {
			_, _ = w.Write([]byte(`jsonpgz();`))
			return
		}
		_, _ = w.Write([]byte(`var db={datas:[],showday:["2026-06-01"]}`))
	}))
	b.Cleanup(server.Close)

	client := NewClient(Options{HTTPClient: server.Client(), NAVBaseURL: server.URL + "/F10DataApi.aspx", MinInterval: time.Nanosecond})
	adapter := NewNAVAdapter(client)
	adapter.estimatedNAVURL = server.URL + "/js/"
	oldDelay := requestPacingDelay
	requestPacingDelay = func() time.Duration { return 0 }
	b.Cleanup(func() { requestPacingDelay = oldDelay })

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		results, err := adapter.fetchNAVs(context.Background(), codes, server.URL+"/Data/Fund_JJJZ_Data.aspx")
		if err != nil {
			b.Fatalf("fetchNAVs returned error: %v", err)
		}
		if len(results) != len(codes) {
			b.Fatalf("fetchNAVs returned %d rows, want %d", len(results), len(codes))
		}
	}
}
