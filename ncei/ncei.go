// Package ncei is the library behind the ncei command line:
// the HTTP client, request shaping, and the typed data models for NOAA NCEI
// (National Centers for Environmental Information) historical climate data.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package ncei

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to NCEI.
const DefaultUserAgent = "ncei/dev (+https://github.com/tamnd/ncei-cli)"

// Host is the NCEI hostname.
const Host = "www.ncei.noaa.gov"

// BaseURL is the root every data request is built from.
const BaseURL = "https://www.ncei.noaa.gov/access/services/data/v1"

// Client talks to NCEI over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// Config holds the tunable client settings returned by DefaultConfig.
type Config struct {
	Rate    time.Duration
	Retries int
	Timeout time.Duration
}

// DefaultConfig returns conservative defaults suitable for a public API.
func DefaultConfig() Config {
	return Config{
		Rate:    500 * time.Millisecond,
		Retries: 3,
		Timeout: 30 * time.Second,
	}
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 500ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: DefaultUserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// MonthlyRecord holds a single monthly climate summary for a station.
type MonthlyRecord struct {
	Station       string  `json:"station" kit:"id"`
	Date          string  `json:"date"`
	TempAvg       float64 `json:"temp_avg_c"`
	Precipitation float64 `json:"precipitation_mm"`
	Snow          float64 `json:"snow_mm"`
	WindSpeed     float64 `json:"wind_speed_ms"`
}

// DailyRecord holds a single daily climate summary for a station.
type DailyRecord struct {
	Station       string  `json:"station" kit:"id"`
	Date          string  `json:"date"`
	TempMax       float64 `json:"temp_max_c"`
	TempMin       float64 `json:"temp_min_c"`
	TempAvg       float64 `json:"temp_avg_c"`
	Precipitation float64 `json:"precipitation_mm"`
	Snow          float64 `json:"snow_mm"`
}

// KnownStation is an entry in the built-in station lookup table.
type KnownStation struct {
	ID       string `json:"id" kit:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// wireRecord is the flat JSON map the NCEI API returns for each row.
type wireRecord map[string]string

// KnownStations is the built-in lookup table of well-known NCEI station IDs.
var KnownStations = []KnownStation{
	{ID: "USW00094728", Name: "Central Park", Location: "New York, NY"},
	{ID: "USW00094846", Name: "JFK Airport", Location: "New York, NY"},
	{ID: "USW00013880", Name: "Hartsfield-Jackson Atlanta International Airport", Location: "Atlanta, GA"},
	{ID: "USW00023234", Name: "Los Angeles International Airport", Location: "Los Angeles, CA"},
	{ID: "USW00094741", Name: "O'Hare International Airport", Location: "Chicago, IL"},
	{ID: "USW00023174", Name: "Seattle-Tacoma International Airport", Location: "Seattle, WA"},
}

// GetMonthly fetches monthly climate summaries for the given station and date range.
// The limit parameter caps results; 0 means no cap.
func (c *Client) GetMonthly(ctx context.Context, station, start, end string, limit int) ([]MonthlyRecord, error) {
	params := url.Values{}
	params.Set("dataset", "global-summary-of-the-month")
	params.Set("stations", station)
	params.Set("startDate", start)
	params.Set("endDate", end)
	params.Set("dataTypes", "TAVG,PRCP,SNOW,AWND")
	params.Set("format", "json")

	raw, err := c.Get(ctx, BaseURL+"?"+params.Encode())
	if err != nil {
		return nil, err
	}

	var wires []wireRecord
	if err := json.Unmarshal(raw, &wires); err != nil {
		return nil, fmt.Errorf("parse monthly response: %w", err)
	}

	var out []MonthlyRecord
	for _, w := range wires {
		// Monthly dataset (global-summary-of-the-month) returns values already
		// in standard units: temperature in °C, precipitation in mm, snow in mm,
		// wind speed in m/s. No unit conversion needed.
		r := MonthlyRecord{
			Station:       w["STATION"],
			Date:          w["DATE"],
			TempAvg:       parseNum(w["TAVG"]),
			Precipitation: parseNum(w["PRCP"]),
			Snow:          parseNum(w["SNOW"]),
			WindSpeed:     parseNum(w["AWND"]),
		}
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// GetDaily fetches daily climate summaries for the given station and date range.
// The limit parameter caps results; 0 means no cap.
func (c *Client) GetDaily(ctx context.Context, station, start, end string, limit int) ([]DailyRecord, error) {
	params := url.Values{}
	params.Set("dataset", "daily-summaries")
	params.Set("stations", station)
	params.Set("startDate", start)
	params.Set("endDate", end)
	params.Set("dataTypes", "TMAX,TMIN,TAVG,PRCP,SNOW")
	params.Set("format", "json")

	raw, err := c.Get(ctx, BaseURL+"?"+params.Encode())
	if err != nil {
		return nil, err
	}

	var wires []wireRecord
	if err := json.Unmarshal(raw, &wires); err != nil {
		return nil, fmt.Errorf("parse daily response: %w", err)
	}

	var out []DailyRecord
	for _, w := range wires {
		// Daily summaries return values in tenths of standard units:
		// temperature in tenths of °C, precipitation in tenths of mm,
		// snow in mm (not tenths). Divide temp and precip by 10.
		r := DailyRecord{
			Station:       w["STATION"],
			Date:          w["DATE"],
			TempMax:       parseNum(w["TMAX"]) / 10,
			TempMin:       parseNum(w["TMIN"]) / 10,
			TempAvg:       parseNum(w["TAVG"]) / 10,
			Precipitation: parseNum(w["PRCP"]) / 10,
			Snow:          parseNum(w["SNOW"]),
		}
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// parseNum parses a string as float64, returning 0 on error.
// NCEI returns numeric values as strings in the JSON response.
func parseNum(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
