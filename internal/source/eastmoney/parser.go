package eastmoney

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gogap/internal/domain"
	"gogap/internal/source"
)

// chinaStandardTime is the CST (UTC+8) timezone.
var chinaStandardTime = time.FixedZone("CST", 8*60*60)

type quotePayload struct {
	Code int `json:"rc"`
	Data struct {
		Diff []quoteEntry `json:"diff"`
	} `json:"data"`
}

type quoteEntry struct {
	Price          json.RawMessage `json:"f2"`
	ChangePercent  json.RawMessage `json:"f3"`
	TurnoverAmount json.RawMessage `json:"f6"`
	TurnoverRate   json.RawMessage `json:"f8"`
	Code           string          `json:"f12"`
	Market         json.RawMessage `json:"f13"`
	Name           string          `json:"f14"`
	MarketValue    json.RawMessage `json:"f20"`
	UnixTime       json.RawMessage `json:"f124"`
}

type navEntry struct {
	Date          string `json:"FSRQ"`
	UnitNAV       string `json:"DWJZ"`
	ChangePercent string `json:"JZZZL"`
}

type navHistoryPayload struct {
	Data struct {
		List []navEntry `json:"LSJZList"`
	} `json:"Data"`
}

type metadataPayload struct {
	Data    [][]string `json:"datas"`
	ShowDay []string   `json:"showday"`
}

type estimatedNAVPayload struct {
	Datas []estimatedNAVEntry `json:"Datas"`
}

type estimatedNAVEntry struct {
	Code          string `json:"FCODE"`
	FundCode      string `json:"fundcode"`
	EstimatedNAV  string `json:"GSZ"`
	JSONPEstimate string `json:"gsz"`
	ChangePercent string `json:"GSZZL"`
	JSONPChange   string `json:"gszzl"`
	EstimateTime  string `json:"GZTIME"`
	JSONPTime     string `json:"gztime"`
}

func ParseQuotePayload(body []byte) (map[string]source.QuoteResult, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var payload quotePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("eastmoney: quote response code %d", payload.Code)
	}
	if len(payload.Data.Diff) == 0 {
		return nil, errors.New("eastmoney: quote data missing")
	}

	results := make(map[string]source.QuoteResult, len(payload.Data.Diff))
	for _, entry := range payload.Data.Diff {
		secID := secID(entry.Market, entry.Code)
		if secID == "" || entry.Code == "" || len(entry.Price) == 0 || len(entry.TurnoverAmount) == 0 {
			continue
		}
		priceValue := rawString(entry.Price)
		tradable := !isEmptyMarketValue(priceValue)
		result := source.QuoteResult{
			Code:               entry.Code,
			Name:               entry.Name,
			Time:               quoteTimeValue(entry.UnixTime),
			TurnoverAmountUnit: "元",
			Tradable:           tradable,
		}
		if tradable {
			price, err := parseRequiredFloat(priceValue, "quote price")
			if err != nil || price <= 0 {
				continue
			}
			turnover, err := parseRequiredFloat(rawString(entry.TurnoverAmount), "quote turnover amount")
			if err != nil || turnover <= 0 {
				continue
			}
			turnoverRate, err := parseRequiredFloat(rawString(entry.TurnoverRate), "quote turnover rate")
			if err != nil {
				continue
			}
			result.Price = &price
			if change, err := parseRequiredFloat(rawString(entry.ChangePercent), "quote change percent"); err == nil {
				result.ChangePercent = &change
			}
			result.TurnoverRate = &turnoverRate
			result.TurnoverAmount = &turnover
			if marketValue, err := parseRequiredFloat(rawString(entry.MarketValue), "quote market value"); err == nil && marketValue > 0 {
				result.MarketValue = &marketValue
			}
		}
		results[secID] = result
	}
	if len(results) == 0 {
		return nil, errors.New("eastmoney: quote data missing")
	}
	return results, nil
}

func ParseNAVPayload(code string, body []byte) (source.NAVResult, error) {
	content := strings.TrimSpace(string(body))
	if content == "" {
		return source.NAVResult{}, errors.New("eastmoney: nav data missing")
	}
	entry, err := parseNAVContent(content)
	if err != nil {
		return source.NAVResult{}, err
	}
	if entry.Date == "" || entry.UnitNAV == "" {
		return source.NAVResult{}, errors.New("eastmoney: nav required field missing")
	}
	nav, err := parseRequiredFloat(entry.UnitNAV, "unit nav")
	if err != nil {
		return source.NAVResult{}, err
	}
	result := source.NAVResult{Code: code, NAV: &nav, NAVDate: entry.Date}
	if change, err := parsePercentValue(entry.ChangePercent, "nav change percent"); err == nil {
		result.ChangePercent = &change
	}
	return result, nil
}

func isEmptyMarketValue(value string) bool {
	return value == "" || value == "--" || value == "-"
}

func ParseMetadataPayload(body []byte) (map[string]source.MetadataResult, error) {
	content := strings.TrimSpace(string(body))
	content = strings.TrimPrefix(content, "var reData=")
	content = strings.TrimPrefix(content, "var db=")
	content = normalizeObjectKeys(content)
	if content == "" {
		return nil, errors.New("eastmoney: metadata data missing")
	}
	var payload metadataPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, err
	}
	results := make(map[string]source.MetadataResult, len(payload.Data))
	for _, row := range payload.Data {
		if len(row) < 10 || row[0] == "" {
			continue
		}
		purchaseStatus := row[5]
		purchaseLimit := ""
		if !isPurchasePaused(purchaseStatus) {
			purchaseLimit = formatPurchaseLimit(row[9])
		}
		results[row[0]] = source.MetadataResult{
			Code:             row[0],
			PurchaseStatus:   purchaseStatus,
			RedemptionStatus: row[6],
			PurchaseLimit:    purchaseLimit,
		}
	}
	return results, nil
}

func ParseLatestNAVPayload(body []byte) (map[string]source.NAVResult, error) {
	content := strings.TrimSpace(string(body))
	content = strings.TrimPrefix(content, "var reData=")
	content = strings.TrimPrefix(content, "var db=")
	content = normalizeObjectKeys(content)
	if content == "" {
		return nil, errors.New("eastmoney: latest nav data missing")
	}
	var payload metadataPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, err
	}
	date := ""
	if len(payload.ShowDay) > 0 {
		date = payload.ShowDay[0]
	}
	results := make(map[string]source.NAVResult, len(payload.Data))
	for _, row := range payload.Data {
		if len(row) < 4 || row[0] == "" || row[3] == "" {
			continue
		}
		if isREITFundType(row[2]) {
			continue
		}
		nav, err := parseRequiredFloat(row[3], "latest nav")
		if err != nil {
			continue
		}
		resultDate := date
		if resultDate == "" && len(row) > 4 {
			resultDate = normalizeShortDate(row[4])
		}
		var changePercent *float64
		if len(row) > 8 && isT1LatestNAVRow(row) {
			if change, err := parseRequiredFloat(row[8], "latest nav change percent"); err == nil {
				changePercent = &change
			}
		}
		results[row[0]] = source.NAVResult{Code: row[0], NAV: &nav, NAVDate: resultDate, ChangePercent: changePercent, Category: categoryFromFundType(row[2], row[1])}
	}
	if len(results) == 0 {
		return nil, errors.New("eastmoney: latest nav data missing")
	}
	return results, nil
}

func ParseEstimatedNAVPayload(body []byte) (map[string]source.NAVResult, error) {
	content := strings.TrimSpace(string(body))
	if content == "" || content == "jsonpgz();" || content == "jsonpgz()" {
		return map[string]source.NAVResult{}, nil
	}
	if strings.HasPrefix(content, "jsonpgz(") {
		content = strings.TrimPrefix(content, "jsonpgz(")
		content = strings.TrimSuffix(content, ");")
		content = strings.TrimSuffix(content, ")")
		var entry estimatedNAVEntry
		if err := json.Unmarshal([]byte(content), &entry); err != nil {
			return nil, err
		}
		return estimatedEntriesToResults([]estimatedNAVEntry{entry}), nil
	}
	var payload estimatedNAVPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, err
	}
	return estimatedEntriesToResults(payload.Datas), nil
}

func estimatedEntriesToResults(entries []estimatedNAVEntry) map[string]source.NAVResult {
	results := make(map[string]source.NAVResult, len(entries))
	for _, entry := range entries {
		code := firstNonEmpty(entry.Code, entry.FundCode)
		estimateValue := firstNonEmpty(entry.EstimatedNAV, entry.JSONPEstimate)
		estimateTime := firstNonEmpty(entry.EstimateTime, entry.JSONPTime)
		if code == "" || estimateValue == "" || estimateTime == "" || estimateValue == "--" {
			continue
		}
		estimatedNAV, err := parseRequiredFloat(estimateValue, "estimated nav")
		if err != nil || estimatedNAV <= 0 {
			continue
		}
		result := source.NAVResult{Code: code, EstimatedNAV: &estimatedNAV, EstimatedNAVTime: estimateTime}
		if change, err := parseRequiredFloat(firstNonEmpty(entry.ChangePercent, entry.JSONPChange), "estimated nav change percent"); err == nil {
			result.EstimatedChangePercent = &change
		}
		results[code] = result
	}
	return results
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ParseFundDiscoveryFromLatestNAVPayload(body []byte) ([]domain.FundSeed, error) {
	content := strings.TrimSpace(string(body))
	content = strings.TrimPrefix(content, "var reData=")
	content = strings.TrimPrefix(content, "var db=")
	content = normalizeObjectKeys(content)
	if content == "" {
		return nil, errors.New("eastmoney: latest nav discovery data missing")
	}
	var payload metadataPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, err
	}
	seeds := make([]domain.FundSeed, 0, len(payload.Data))
	for _, row := range payload.Data {
		if len(row) < 3 || row[0] == "" || row[1] == "" || isTerminatedFundName(row[1]) || isREITFundType(row[2]) {
			continue
		}
		market := listedFundMarket(row[0])
		if market == "" {
			continue
		}
		category := categoryFromFundType(row[2], row[1])
		if category == "" || category == domain.CategoryBondMoney {
			continue
		}
		seeds = append(seeds, domain.FundSeed{
			Code:       row[0],
			Name:       row[1],
			Category:   category,
			Exchange:   exchangeName(market),
			QuoteSecID: market + "." + row[0],
			NAVCode:    row[0],
			Enabled:    true,
		})
	}
	if len(seeds) == 0 {
		return nil, errors.New("eastmoney: latest nav discovery data missing")
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Code < seeds[j].Code })
	return seeds, nil
}

func listedFundMarket(code string) string {
	if len(code) != 6 {
		return ""
	}
	switch {
	case strings.HasPrefix(code, "15"), strings.HasPrefix(code, "16"), strings.HasPrefix(code, "18"):
		return "0"
	case strings.HasPrefix(code, "50"), strings.HasPrefix(code, "51"), strings.HasPrefix(code, "52"), strings.HasPrefix(code, "56"), strings.HasPrefix(code, "58"):
		return "1"
	default:
		return ""
	}
}

func ParseListedNAVPayload(body []byte) (map[string]source.NAVResult, error) {
	content := normalizedHTMLText(body)
	rowPattern := regexp.MustCompile(`(\d{6})([^\d]*?)(指数型-[^\d]+)(\d+\.\d{4})(\d+\.\d{4})([-+]?\d+\.\d+)([-+]?\d+(?:\.\d+)?)%`)
	matches := rowPattern.FindAllStringSubmatch(content, -1)
	results := make(map[string]source.NAVResult, len(matches))
	for _, match := range matches {
		change, err := parseRequiredFloat(match[7], "listed nav change percent")
		if err != nil {
			continue
		}
		results[match[1]] = source.NAVResult{Code: match[1], ChangePercent: &change}
	}
	if len(results) == 0 {
		return nil, errors.New("eastmoney: listed nav data missing")
	}
	return results, nil
}

func isT1LatestNAVRow(row []string) bool {
	if len(row) < 9 {
		return false
	}
	if _, err := parseRequiredFloat(row[7], "latest nav delta"); err != nil {
		return false
	}
	_, err := parseRequiredFloat(row[8], "latest nav change percent")
	return err == nil
}

func categoryFromFundType(fundType string, fundName string) domain.FundCategory {
	upper := strings.ToUpper(strings.TrimSpace(fundType))
	name := strings.TrimSpace(fundName)
	switch {
	case strings.Contains(name, "香港") || strings.Contains(name, "港股") || strings.Contains(name, "恒生") || strings.Contains(fundType, "香港") || strings.Contains(fundType, "港股") || strings.Contains(fundType, "恒生"):
		return domain.CategoryHongKong
	case strings.Contains(name, "商品") || strings.Contains(name, "黄金") || strings.Contains(name, "白银") || strings.Contains(name, "原油") || strings.Contains(name, "能源") || strings.Contains(fundType, "商品") || strings.Contains(fundType, "黄金") || strings.Contains(fundType, "白银") || strings.Contains(fundType, "原油") || strings.Contains(fundType, "能源"):
		return domain.CategoryCommodity
	case strings.Contains(upper, "QDII"):
		return domain.CategoryQDII
	case strings.Contains(fundType, "海外") || strings.Contains(name, "纳指") || strings.Contains(name, "标普"):
		return domain.CategoryQDII
	case strings.Contains(fundType, "货币") || strings.Contains(fundType, "债券") || strings.Contains(fundType, "短期理财"):
		return domain.CategoryBondMoney
	case strings.Contains(fundType, "指数"):
		return domain.CategoryIndexLOF
	case strings.Contains(fundType, "混合") || strings.Contains(fundType, "股票"):
		return domain.CategoryActiveLOF
	default:
		return ""
	}
}

func isREITFundType(fundType string) bool {
	return strings.Contains(strings.ToUpper(strings.TrimSpace(fundType)), "REIT")
}

func isPurchasePaused(status string) bool {
	return strings.Contains(status, "暂停申购")
}

func ParseFundOverviewPayload(code string, body []byte) source.MetadataResult {
	content := normalizedHTMLText(body)
	return source.MetadataResult{
		Code:             code,
		PurchaseStatus:   firstSubmatch(content, regexp.MustCompile(`交易状态：((?:开放|暂停|限制|限大额)申购)`)),
		RedemptionStatus: firstSubmatch(content, regexp.MustCompile(`(开放赎回|暂停赎回)`)),
		PurchaseLimit:    overviewPurchaseLimit(content),
		FundScale:        firstSubmatch(content, regexp.MustCompile(`净资产规模：([0-9,.]+亿元)`)),
		FundScaleDate:    normalizeDate(firstSubmatch(content, regexp.MustCompile(`净资产规模：[0-9,.]+亿元（截止至：([0-9年月日-]+)`))),
	}
}

func overviewPurchaseLimit(content string) string {
	status := firstSubmatch(content, regexp.MustCompile(`交易状态：((?:开放|暂停|限制|限大额)申购)`))
	if isPurchasePaused(status) {
		return ""
	}
	return firstSubmatch(content, regexp.MustCompile(`单日累计购买上限([0-9.]+[万亿元]*)`))
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func normalizedHTMLText(body []byte) string {
	content := htmlTagPattern.ReplaceAllString(string(body), " ")
	content = html.UnescapeString(content)
	return strings.Join(strings.Fields(content), "")
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "日")
	value = strings.ReplaceAll(value, "年", "-")
	value = strings.ReplaceAll(value, "月", "-")
	return value
}

func normalizeShortDate(value string) string {
	value = strings.TrimSpace(value)
	if regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(value) {
		return value
	}
	if regexp.MustCompile(`^\d{2}-\d{2}$`).MatchString(value) {
		return fmt.Sprintf("%d-%s", time.Now().In(chinaStandardTime).Year(), value)
	}
	return value
}

func normalizeObjectKeys(content string) string {
	for _, key := range []string{"chars", "datas", "count", "record", "pages", "curpage", "indexsy", "showday"} {
		content = regexp.MustCompile(`([,{])`+key+`:`).ReplaceAllString(content, `${1}"`+key+`":`)
	}
	content = strings.ReplaceAll(content, ",]", "]")
	return content
}

var navContentPattern = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\D*(\d+\.\d{4})(?:[\s\S]*?([-+]?\d+(?:\.\d+)?)%)?`)

func parseNAVContent(content string) (navEntry, error) {
	if strings.HasPrefix(content, "{") {
		var payload navHistoryPayload
		if err := json.Unmarshal([]byte(content), &payload); err == nil && len(payload.Data.List) > 0 {
			return payload.Data.List[0], nil
		}
	}
	match := navContentPattern.FindStringSubmatch(content)
	if len(match) < 3 {
		return navEntry{}, errors.New("eastmoney: nav content missing date or unit nav")
	}
	entry := navEntry{Date: match[1], UnitNAV: match[2]}
	if len(match) > 3 {
		entry.ChangePercent = match[3]
	}
	return entry, nil
}

func parsePercentValue(value string, field string) (float64, error) {
	return parseRequiredFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), field)
}

func firstSubmatch(content string, pattern *regexp.Regexp) string {
	match := pattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func formatPurchaseLimit(value string) string {
	if value == "" || value == "0" || strings.HasPrefix(value, "999999999") || strings.HasPrefix(value, "100000000") {
		return ""
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return value
	}
	if amount >= 10000 {
		return fmt.Sprintf("%.0f万", amount/10000)
	}
	return fmt.Sprintf("%.0f元", amount)
}

func quoteTime(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).In(chinaStandardTime).Format(time.RFC3339)
}

func quoteTimeValue(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	parsed, err := strconv.ParseInt(rawString(value), 10, 64)
	if err != nil {
		return ""
	}
	return quoteTime(parsed)
}

func parseRequiredFloat(value, name string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "--" {
		return 0, fmt.Errorf("eastmoney: %s missing", name)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("eastmoney: parse %s: %w", name, err)
	}
	return parsed, nil
}

func normalizeTurnoverUnit(unit string) string {
	if unit == "1" {
		return "元"
	}
	return unit
}

func secID(market json.RawMessage, code string) string {
	if len(market) == 0 || code == "" {
		return ""
	}
	return rawString(market) + "." + code
}

func rawString(value json.RawMessage) string {
	trimmed := strings.TrimSpace(string(value))
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		return trimmed[1 : len(trimmed)-1]
	}
	return trimmed
}
