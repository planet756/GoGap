package eastmoney

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"gogap/internal/source"
)

const (
	defaultQuoteBaseURL  = "https://push2delay.eastmoney.com/api/qt/ulist.np/get"
	httpQuoteBaseURL     = "http://push2.eastmoney.com/api/qt/ulist.np/get"
	numberedQuoteBaseURL = "https://82.push2.eastmoney.com/api/qt/ulist.np/get"
	primaryQuoteBaseURL  = "https://push2.eastmoney.com/api/qt/ulist.np/get"
	defaultNAVBaseURL    = "https://fundf10.eastmoney.com/F10DataApi.aspx"
	userAgent            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36"
	akShareMaxRetries    = 3
	akShareRetryBase     = time.Second
)

var retryBackoffDelay = func(attempt int) time.Duration {
	base := akShareRetryBase * time.Duration(1<<attempt)
	jitter := akShareRequestDelayMin + time.Duration(rand.Int64N(int64(akShareRequestDelayMax-akShareRequestDelayMin)+1))
	return base + jitter
}

var ErrCircuitOpen = errors.New("eastmoney: circuit breaker open")

type Clock interface {
	Now() time.Time
}

type CacheFallback interface {
	Quote(ctx context.Context, secID string) (CachedQuote, bool, error)
	NAV(ctx context.Context, code string) (CachedNAV, bool, error)
}

type CachedQuote struct {
	Result source.QuoteResult
	Stale  bool
}

type CachedNAV struct {
	Result source.NAVResult
	Stale  bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Options struct {
	HTTPClient      *http.Client
	QuoteBaseURL    string
	QuoteBaseURLs   []string
	NAVBaseURL      string
	Clock           Clock
	MinInterval     time.Duration
	BreakerCooldown time.Duration
	CacheFallback   CacheFallback
}

type Client struct {
	httpClient      *http.Client
	quoteBaseURL    string
	quoteBaseURLs   []string
	navBaseURL      string
	clock           Clock
	minInterval     time.Duration
	breakerCooldown time.Duration
	cacheFallback   CacheFallback

	mu      sync.Mutex
	domains map[string]*domainState
}

type domainState struct {
	lastRequest time.Time
	failures    int
	openUntil   time.Time
}

func NewClient(opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:   15 * time.Second,
			Transport: eastMoneyTransport(),
		}
	} else if httpClient.Timeout == 0 {
		clone := *httpClient
		clone.Timeout = 15 * time.Second
		if clone.Transport == nil {
			clone.Transport = eastMoneyTransport()
		}
		httpClient = &clone
	} else if httpClient.Transport == nil {
		clone := *httpClient
		clone.Transport = eastMoneyTransport()
		httpClient = &clone
	}

	clock := opts.Clock
	if clock == nil {
		clock = realClock{}
	}

	minInterval := opts.MinInterval
	if minInterval == 0 {
		minInterval = akShareRequestDelayMin
	}

	cooldown := opts.BreakerCooldown
	if cooldown == 0 {
		cooldown = 30 * time.Second
	}

	quoteBaseURL := opts.QuoteBaseURL
	if quoteBaseURL == "" {
		quoteBaseURL = defaultQuoteBaseURL
	}
	quoteBaseURLs := append([]string{}, opts.QuoteBaseURLs...)
	if len(quoteBaseURLs) == 0 {
		quoteBaseURLs = []string{quoteBaseURL, numberedQuoteBaseURL, primaryQuoteBaseURL, httpQuoteBaseURL}
	}
	navBaseURL := opts.NAVBaseURL
	if navBaseURL == "" {
		navBaseURL = defaultNAVBaseURL
	}

	return &Client{
		httpClient:      httpClient,
		quoteBaseURL:    quoteBaseURL,
		quoteBaseURLs:   quoteBaseURLs,
		navBaseURL:      navBaseURL,
		clock:           clock,
		minInterval:     minInterval,
		breakerCooldown: cooldown,
		cacheFallback:   opts.CacheFallback,
		domains:         map[string]*domainState{},
	}
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if err := c.beforeRequest(ctx, parsed.Host); err != nil {
		return nil, err
	}

	body, statusCode, err := c.getWithRetry(ctx, rawURL)
	if err != nil {
		c.recordFailure(parsed.Host)
		return nil, err
	}
	if statusCode < 200 || statusCode >= 300 {
		c.recordFailure(parsed.Host)
		return nil, fmt.Errorf("eastmoney: upstream status %d", statusCode)
	}

	c.recordSuccess(parsed.Host)
	return body, nil
}

func (c *Client) getWithRetry(ctx context.Context, rawURL string) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt < akShareMaxRetries; attempt++ {
		body, statusCode, err := c.doGet(ctx, rawURL)
		if err == nil {
			return body, statusCode, nil
		}
		lastErr = err
		if attempt < akShareMaxRetries-1 && isTransientTransportError(err) {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(retryBackoffDelay(attempt)):
			}
			continue
		}
		break
	}
	return nil, 0, lastErr
}

func (c *Client) doGet(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	setEastMoneyHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, 0, readErr
	}
	return body, resp.StatusCode, nil
}

func isTransientTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return false
}

func eastMoneyTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.DisableKeepAlives = false
	transport.ForceAttemptHTTP2 = false
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 4
	transport.IdleConnTimeout = 90 * time.Second
	return transport
}

func setEastMoneyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Origin", "https://quote.eastmoney.com")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
}

func (c *Client) beforeRequest(ctx context.Context, host string) error {
	var wait time.Duration

	c.mu.Lock()
	state := c.state(host)
	now := c.clock.Now()
	if !state.openUntil.IsZero() && now.Before(state.openUntil) {
		c.mu.Unlock()
		return ErrCircuitOpen
	}
	if !state.openUntil.IsZero() && !now.Before(state.openUntil) {
		state.openUntil = time.Time{}
		state.failures = 0
	}
	if !state.lastRequest.IsZero() {
		next := state.lastRequest.Add(c.minInterval)
		if now.Before(next) {
			wait = next.Sub(now)
		}
	}
	state.lastRequest = now.Add(wait)
	c.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) recordFailure(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state(host)
	state.failures++
	if state.failures >= 3 {
		state.openUntil = c.clock.Now().Add(c.breakerCooldown)
	}
}

func (c *Client) recordSuccess(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state(host)
	state.failures = 0
	state.openUntil = time.Time{}
}

func (c *Client) state(host string) *domainState {
	state := c.domains[host]
	if state == nil {
		state = &domainState{}
		c.domains[host] = state
	}
	return state
}

func appendQuery(base string, values url.Values) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, items := range values {
		for _, item := range items {
			query.Add(key, item)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func codeFromSecID(secID string) string {
	if idx := strings.LastIndex(secID, "."); idx >= 0 && idx+1 < len(secID) {
		return secID[idx+1:]
	}
	return secID
}
