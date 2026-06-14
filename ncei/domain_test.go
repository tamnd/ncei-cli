package ncei

import (
	"testing"
)

// These tests are offline: they exercise the URI driver's pure string functions.
// The client's HTTP behaviour is covered in ncei_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "ncei" {
		t.Errorf("Scheme = %q, want ncei", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "ncei" {
		t.Errorf("Identity.Binary = %q, want ncei", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"USW00094728", "station", "USW00094728"},
		{"USW00094846", "station", "USW00094846"},
		{"monthly", "query", "monthly"},
		{"daily", "query", "daily"},
		{"stations", "query", "stations"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q) error: %v", tc.in, err)
			continue
		}
		if typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)",
				tc.in, typ, id, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("station", "USW00094728")
	want := "https://www.ncei.noaa.gov/cdo-web/datasets/GHCND/stations/GHCND:USW00094728/detail"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}

	_, err = Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("Locate(unknown) should return error")
	}
}

func TestLooksLikeStation(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"USW00094728", true},
		{"USW00094846", true},
		{"USW00013880", true},
		{"monthly", false},
		{"daily", false},
		{"", false},
		{"US", false},
	}
	for _, tc := range cases {
		got := looksLikeStation(tc.in)
		if got != tc.want {
			t.Errorf("looksLikeStation(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	station, start, end := resolveDefaults("", "", "")
	if station != "USW00094728" {
		t.Errorf("default station = %q, want USW00094728", station)
	}
	if start == "" {
		t.Error("default start is empty")
	}
	if end == "" {
		t.Error("default end is empty")
	}

	// Explicit values pass through unchanged.
	s2, st2, en2 := resolveDefaults("USW00094846", "2024-01-01", "2024-12-31")
	if s2 != "USW00094846" {
		t.Errorf("station = %q, want USW00094846", s2)
	}
	if st2 != "2024-01-01" {
		t.Errorf("start = %q, want 2024-01-01", st2)
	}
	if en2 != "2024-12-31" {
		t.Errorf("end = %q, want 2024-12-31", en2)
	}
}
