package ncei_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/ncei-cli/ncei"
)

func TestGetMonthlyParsing(t *testing.T) {
	// Monthly dataset returns values in standard units (no conversion needed).
	// PRCP and SNOW are in mm, TAVG in °C, AWND in m/s.
	payload := []map[string]string{
		{"DATE": "2024-01", "STATION": "USW00094728", "TAVG": "2.79", "PRCP": "134.1", "SNOW": "58", "AWND": "3.0"},
		{"DATE": "2024-02", "STATION": "USW00094728", "TAVG": "4.49", "PRCP": "52.1", "SNOW": "132", "AWND": "2.6"},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := ncei.NewClient()
	c.Rate = 0

	ctx := context.Background()
	raw, err := c.Get(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	var wires []map[string]string
	if err := json.Unmarshal(raw, &wires); err != nil {
		t.Fatal(err)
	}
	if len(wires) != 2 {
		t.Fatalf("got %d records, want 2", len(wires))
	}

	// TAVG "2.79" -> already 2.79 °C (no division for monthly).
	if got := wires[0]["TAVG"]; got != "2.79" {
		t.Errorf("TAVG = %q, want 2.79", got)
	}
	// PRCP "134.1" -> already 134.1 mm (no division for monthly).
	if got := wires[0]["PRCP"]; got != "134.1" {
		t.Errorf("PRCP = %q, want 134.1", got)
	}
	// SNOW "132" -> 132 mm in February.
	if got := wires[1]["SNOW"]; got != "132" {
		t.Errorf("SNOW = %q, want 132", got)
	}
}

func TestGetDailyParsing(t *testing.T) {
	// Daily-summaries returns values in tenths of standard units.
	// TMAX "83" = 8.3°C, PRCP "8" = 0.8mm, SNOW in mm (not tenths).
	payload := []map[string]string{
		{"DATE": "2024-01-01", "STATION": "USW00094728", "TMAX": "83", "TMIN": "17", "PRCP": "8", "SNOW": "0"},
		{"DATE": "2024-01-02", "STATION": "USW00094728", "TMAX": "56", "TMIN": "-16", "PRCP": "0", "SNOW": "0"},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := ncei.NewClient()
	c.Rate = 0

	ctx := context.Background()
	raw, err := c.Get(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	var wires []map[string]string
	if err := json.Unmarshal(raw, &wires); err != nil {
		t.Fatal(err)
	}
	if len(wires) != 2 {
		t.Fatalf("got %d records, want 2", len(wires))
	}

	// TMAX "83" -> 83/10 = 8.3°C when parsed by the client.
	// Here we verify the raw wire value before client processing.
	if got := wires[0]["TMAX"]; got != "83" {
		t.Errorf("TMAX wire = %q, want 83", got)
	}
	// Negative temperature TMIN "-16" -> -16/10 = -1.6°C.
	if got := wires[1]["TMIN"]; got != "-16" {
		t.Errorf("TMIN wire = %q, want -16", got)
	}
}

func TestRetry503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := ncei.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `[]` {
		t.Errorf("body = %q, want []", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestKnownStations(t *testing.T) {
	stations := ncei.KnownStations
	if len(stations) == 0 {
		t.Fatal("KnownStations is empty")
	}
	found := false
	for _, s := range stations {
		if s.ID == "USW00094728" {
			found = true
			if s.Name == "" {
				t.Error("Central Park station has empty Name")
			}
			if s.Location == "" {
				t.Error("Central Park station has empty Location")
			}
		}
	}
	if !found {
		t.Error("USW00094728 (Central Park) not in KnownStations")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := ncei.DefaultConfig()
	if cfg.Rate != 500*time.Millisecond {
		t.Errorf("Rate = %v, want 500ms", cfg.Rate)
	}
	if cfg.Retries != 3 {
		t.Errorf("Retries = %d, want 3", cfg.Retries)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
}
