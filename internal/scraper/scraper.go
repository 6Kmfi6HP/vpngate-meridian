package scraper

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/6Kmfi6HP/vpngate-scraper/internal/config"
	"github.com/6Kmfi6HP/vpngate-scraper/internal/csvparser"
)

const baseURL = "http://www.vpngate.net/api/iphone/"

// Scraper fetches and parses VPN Gate API data.
type Scraper struct {
	client  *http.Client
	timeout time.Duration
}

// New creates a Scraper with the given config.
func New(cfg *config.Config) *Scraper {
	return &Scraper{
		client: &http.Client{
			Timeout: cfg.RequestTimeout(),
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		timeout: cfg.RequestTimeout(),
	}
}

// Fetch makes a single API request and parses the response.
func (s *Scraper) Fetch(ctx context.Context) (*csvparser.ParseResult, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	q := u.Query()
	q.Set("t", fmt.Sprintf("%d", time.Now().UnixMilli()))
	q.Set("nonce", RandomString(10))
	q.Set("r", fmt.Sprintf("%f", rand.Float64()))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header = BuildHeaders()

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	data := string(body)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return csvparser.Parse(data)
}
