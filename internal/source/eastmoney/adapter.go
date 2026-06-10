package eastmoney

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	"gogap/internal/source"
)

type QuoteAdapter struct {
	client *Client
}

type NAVAdapter struct {
	client          *Client
	listedNAVURL    string
	estimatedNAVURL string
}

type MetadataAdapter struct {
	client              *Client
	purchaseMetadataURL string
	fundOverviewURLBase string
}

const quoteBatchSize = 60
const quoteBatchConcurrency = 4
const estimateRequestConcurrency = 32
const navFallbackLargeUniverseThreshold = 200

const (
	akShareRequestDelayMin = 500 * time.Millisecond
	akShareRequestDelayMax = 1500 * time.Millisecond
)

var requestPacingDelay = func() time.Duration {
	return akShareRequestDelayMin + time.Duration(rand.Int64N(int64(akShareRequestDelayMax-akShareRequestDelayMin)+1))
}

var estimateFetchTimeout = 3 * time.Second

var overviewRequestInterval = 120 * time.Millisecond

func NewQuoteAdapter(client *Client) *QuoteAdapter {
	if client == nil {
		client = NewClient(Options{})
	}
	return &QuoteAdapter{client: client}
}

func NewNAVAdapter(client *Client) *NAVAdapter {
	if client == nil {
		client = NewClient(Options{})
	}
	return &NAVAdapter{client: client, listedNAVURL: "https://fund.eastmoney.com/cnjy_dwjz.html", estimatedNAVURL: "https://fundgz.1234567.com.cn/js/"}
}

func NewMetadataAdapter(client *Client) *MetadataAdapter {
	if client == nil {
		client = NewClient(Options{})
	}
	return &MetadataAdapter{client: client, purchaseMetadataURL: "https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx", fundOverviewURLBase: "https://fundf10.eastmoney.com/jbgk_"}
}

func (a *QuoteAdapter) FetchQuotes(ctx context.Context, secIDs []string) (map[string]source.QuoteResult, error) {
	results := make(map[string]source.QuoteResult, len(secIDs))
	if len(secIDs) == 0 {
		return results, nil
	}
	batches := make([][]string, 0, (len(secIDs)+quoteBatchSize-1)/quoteBatchSize)
	for start := 0; start < len(secIDs); start += quoteBatchSize {
		end := start + quoteBatchSize
		if end > len(secIDs) {
			end = len(secIDs)
		}
		batches = append(batches, secIDs[start:end])
	}

	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup
	limit := make(chan struct{}, quoteBatchConcurrency)
	for _, batch := range batches {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		wg.Add(1)
		go func(batch []string) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}
			batchResults, err := a.fetchQuoteBatch(ctx, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err.Error())
				return
			}
			for secID, result := range batchResults {
				results[secID] = result
			}
		}(batch)
	}
	wg.Wait()
	if ctx.Err() != nil && len(results) == 0 {
		return nil, ctx.Err()
	}
	if len(results) == 0 {
		fallback, ok, fallbackErr := quoteFallback(ctx, a.client.cacheFallback, secIDs)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		if ok {
			return fallback, nil
		}
		return nil, errors.New(strings.Join(failures, "; "))
	}
	return results, nil
}

func (a *QuoteAdapter) fetchQuoteBatch(ctx context.Context, secIDs []string) (map[string]source.QuoteResult, error) {
	var failures []string
	for _, baseURL := range a.client.quoteBaseURLs {
		endpoint, err := appendQuery(baseURL, url.Values{
			"secids": {strings.Join(secIDs, ",")},
			"fltt":   {"2"},
			"invt":   {"2"},
			"fields": {"f2,f3,f6,f8,f12,f13,f14,f20,f124"},
			"ut":     {"fa5fd1943c7b386f172d6893dbfba10b"},
			"_":      {fmt.Sprintf("%d", time.Now().UnixMilli())},
		})
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}

		body, err := a.client.get(ctx, endpoint)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		parsed, err := ParseQuotePayload(body)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}

		results := make(map[string]source.QuoteResult, len(secIDs))
		for _, secID := range secIDs {
			if result, ok := parsed[secID]; ok {
				results[secID] = result
			}
		}
		return results, nil
	}
	return nil, errors.New(strings.Join(failures, "; "))
}

func (a *NAVAdapter) FetchNAVs(ctx context.Context, fundCodes []string) (map[string]source.NAVResult, error) {
	return a.fetchNAVs(ctx, fundCodes, "https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx")
}

func (a *NAVAdapter) fetchNAVs(ctx context.Context, fundCodes []string, latestURL string) (map[string]source.NAVResult, error) {
	results := make(map[string]source.NAVResult, len(fundCodes))
	latest, err := a.fetchLatestNAVs(ctx, latestURL)
	if err == nil {
		changes := map[string]source.NAVResult{}
		if latestT1, t1Err := a.fetchLatestNAVsWithParams(ctx, latestURL, latestNAVChangeParams()); t1Err == nil {
			changes = latestT1
		}
		for _, code := range fundCodes {
			if result, ok := latest[code]; ok {
				results[code] = mergeNAVChangePercent(result, changes[code])
			}
		}
		if len(results) == len(fundCodes) {
			a.mergeEstimatedNAVs(ctx, fundCodes, results)
			return results, nil
		}
		if len(fundCodes) > navFallbackLargeUniverseThreshold {
			a.mergeEstimatedNAVs(ctx, keysOfNAVResults(results), results)
			return results, nil
		}
	}
	var failures []string
	for index, code := range fundCodes {
		if _, ok := results[code]; ok {
			continue
		}
		if err := waitBetweenRequests(ctx, index > 0); err != nil {
			return nil, err
		}
		result, err := a.fetchFallbackNAV(ctx, code)
		if err != nil {
			if a.client.cacheFallback != nil {
				cached, ok, fallbackErr := a.client.cacheFallback.NAV(ctx, code)
				if fallbackErr != nil {
					return nil, fallbackErr
				}
				if ok {
					results[code] = cached.Result
					continue
				}
			}
			failures = append(failures, fmt.Sprintf("%s: %v", code, err))
			continue
		}
		results[code] = result
	}
	a.mergeEstimatedNAVs(ctx, fundCodes, results)
	if len(results) == 0 && len(failures) != 0 {
		return nil, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return results, nil
}

func keysOfNAVResults(results map[string]source.NAVResult) []string {
	codes := make([]string, 0, len(results))
	for code := range results {
		codes = append(codes, code)
	}
	return codes
}

func (a *NAVAdapter) mergeEstimatedNAVs(ctx context.Context, fundCodes []string, results map[string]source.NAVResult) {
	estimates, err := a.fetchEstimatedNAVs(ctx, fundCodes)
	if err != nil {
		return
	}
	for code, estimate := range estimates {
		current, ok := results[code]
		if !ok {
			continue
		}
		current.EstimatedNAV = cloneFloat64(estimate.EstimatedNAV)
		current.EstimatedNAVTime = estimate.EstimatedNAVTime
		current.EstimatedChangePercent = cloneFloat64(estimate.EstimatedChangePercent)
		results[code] = current
	}
}

func (a *NAVAdapter) fetchEstimatedNAVs(ctx context.Context, fundCodes []string) (map[string]source.NAVResult, error) {
	results := make(map[string]source.NAVResult, len(fundCodes))
	if len(fundCodes) == 0 || a.estimatedNAVURL == "" {
		return results, nil
	}
	estimateCtx, cancel := context.WithTimeout(ctx, estimateFetchTimeout)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, estimateRequestConcurrency)
	for _, code := range fundCodes {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-estimateCtx.Done():
				return
			}
			endpoint, err := appendQuery(a.estimatedNAVURL+code+".js", url.Values{"rt": {fmt.Sprintf("%d", time.Now().UnixMilli())}})
			if err != nil {
				return
			}
			body, statusCode, err := a.client.doGet(estimateCtx, endpoint)
			if err != nil || statusCode < 200 || statusCode >= 300 {
				return
			}
			parsed, err := ParseEstimatedNAVPayload(body)
			if err != nil {
				return
			}
			mu.Lock()
			for code, result := range parsed {
				results[code] = result
			}
			mu.Unlock()
		}(code)
	}
	wg.Wait()
	return results, nil
}

func (a *NAVAdapter) FillMissingChangePercent(ctx context.Context, navs map[string]source.NAVResult, fundCodes []string) (map[string]source.NAVResult, error) {
	results := make(map[string]source.NAVResult, len(navs))
	for code, result := range navs {
		results[code] = result
	}
	if err := a.fillListedNAVChangePercent(ctx, fundCodes, results); err != nil {
		return results, err
	}
	return results, a.fillMissingNAVChangePercent(ctx, fundCodes, results)
}

func (a *NAVAdapter) fillListedNAVChangePercent(ctx context.Context, fundCodes []string, results map[string]source.NAVResult) error {
	body, err := a.client.get(ctx, a.listedNAVURL)
	if err != nil {
		return nil
	}
	listed, err := ParseListedNAVPayload(body)
	if err != nil {
		return nil
	}
	for _, code := range fundCodes {
		result, ok := results[code]
		if !ok || result.ChangePercent != nil {
			continue
		}
		results[code] = mergeNAVChangePercent(result, listed[code])
	}
	return nil
}

func (a *NAVAdapter) fillMissingNAVChangePercent(ctx context.Context, fundCodes []string, results map[string]source.NAVResult) error {
	requestIndex := 0
	for _, code := range fundCodes {
		result, ok := results[code]
		if !ok || result.ChangePercent != nil {
			continue
		}
		if err := waitBetweenRequests(ctx, requestIndex > 0); err != nil {
			return err
		}
		requestIndex++
		fallback, err := a.fetchFallbackNAV(ctx, code)
		if err != nil {
			continue
		}
		results[code] = mergeNAVChangePercent(result, fallback)
	}
	return nil
}

func (a *NAVAdapter) fetchFallbackNAV(ctx context.Context, code string) (source.NAVResult, error) {
	endpoint, err := appendQuery(a.client.navBaseURL, url.Values{
		"type":  {"lsjz"},
		"code":  {code},
		"page":  {"1"},
		"per":   {"1"},
		"sdate": {""},
		"edate": {""},
	})
	if err != nil {
		return source.NAVResult{}, err
	}
	body, err := a.client.get(ctx, endpoint)
	if err != nil {
		return source.NAVResult{}, err
	}
	return ParseNAVPayload(code, body)
}

func mergeNAVChangePercent(result source.NAVResult, change source.NAVResult) source.NAVResult {
	if result.ChangePercent == nil {
		result.ChangePercent = cloneFloat64(change.ChangePercent)
	}
	return result
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (a *NAVAdapter) fetchLatestNAVs(ctx context.Context, baseURL string) (map[string]source.NAVResult, error) {
	return a.fetchLatestNAVsWithParams(ctx, baseURL, latestNAVParams())
}

func (a *NAVAdapter) fetchLatestNAVsWithParams(ctx context.Context, baseURL string, params url.Values) (map[string]source.NAVResult, error) {
	endpoint, err := appendQuery(baseURL, params)
	if err != nil {
		return nil, err
	}
	body, err := a.client.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return ParseLatestNAVPayload(body)
}

func latestNAVParams() url.Values {
	return url.Values{
		"t":    {"8"},
		"page": {"1,50000"},
		"sort": {"fcode,asc"},
		"js":   {"reData"},
	}
}

func latestNAVChangeParams() url.Values {
	return url.Values{
		"t":    {"1"},
		"page": {"1,50000"},
		"sort": {"fcode,asc"},
		"js":   {"db"},
	}
}

func (a *MetadataAdapter) FetchMetadata(ctx context.Context, fundCodes []string) (map[string]source.MetadataResult, error) {
	return a.FetchMetadataWithProgress(ctx, fundCodes, nil)
}

func (a *MetadataAdapter) FetchMetadataWithProgress(ctx context.Context, fundCodes []string, onProgress func(done int, total int)) (map[string]source.MetadataResult, error) {
	return a.fetchMetadata(ctx, fundCodes, a.purchaseMetadataURL, a.fundOverviewURLBase, onProgress)
}

func (a *MetadataAdapter) fetchMetadata(ctx context.Context, fundCodes []string, purchaseURL string, overviewURLBase string, onProgress func(done int, total int)) (map[string]source.MetadataResult, error) {
	results := make(map[string]source.MetadataResult, len(fundCodes))
	metadata, err := a.fetchPurchaseMetadata(ctx, purchaseURL)
	if err == nil {
		for _, code := range fundCodes {
			if result, ok := metadata[code]; ok {
				results[code] = result
			}
		}
	}
	if len(fundCodes) > 200 {
		return results, nil
	}
	for index, code := range fundCodes {
		if err := waitForInterval(ctx, overviewRequestInterval, index > 0); err != nil {
			return nil, err
		}
		endpoint, err := appendQuery(overviewURLBase+code+".html", nil)
		if err != nil {
			continue
		}
		body, err := a.client.get(ctx, endpoint)
		if err != nil {
			continue
		}
		overview := ParseFundOverviewPayload(code, body)
		current := results[code]
		results[code] = mergeMetadata(current, overview)
		if onProgress != nil {
			onProgress(index+1, len(fundCodes))
		}
	}
	return results, nil
}

func (a *MetadataAdapter) fetchPurchaseMetadata(ctx context.Context, baseURL string) (map[string]source.MetadataResult, error) {
	endpoint, err := appendQuery(baseURL, url.Values{
		"t":    {"8"},
		"page": {"1,50000"},
		"js":   {"reData"},
		"sort": {"fcode,asc"},
	})
	if err != nil {
		return nil, err
	}
	body, err := a.client.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return ParseMetadataPayload(body)
}

func waitBetweenRequests(ctx context.Context, wait bool) error {
	return waitForInterval(ctx, requestPacingDelay(), wait)
}

func waitForInterval(ctx context.Context, interval time.Duration, wait bool) error {
	if !wait {
		return ctx.Err()
	}
	if interval <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func mergeMetadata(base, next source.MetadataResult) source.MetadataResult {
	if base.Code == "" {
		base.Code = next.Code
	}
	if next.PurchaseStatus != "" {
		base.PurchaseStatus = next.PurchaseStatus
	}
	if next.RedemptionStatus != "" {
		base.RedemptionStatus = next.RedemptionStatus
	}
	base.PurchaseLimit = next.PurchaseLimit
	if next.FundScale != "" {
		base.FundScale = next.FundScale
	}
	if next.FundScaleDate != "" {
		base.FundScaleDate = next.FundScaleDate
	}
	return base
}

func quoteFallback(ctx context.Context, fallback CacheFallback, secIDs []string) (map[string]source.QuoteResult, bool, error) {
	if fallback == nil {
		return nil, false, nil
	}
	results := make(map[string]source.QuoteResult, len(secIDs))
	for _, secID := range secIDs {
		cached, ok, err := fallback.Quote(ctx, secID)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		results[secID] = cached.Result
	}
	return results, true, nil
}

var _ source.QuoteAdapter = (*QuoteAdapter)(nil)
var _ source.NAVAdapter = (*NAVAdapter)(nil)
var _ source.MetadataAdapter = (*MetadataAdapter)(nil)
