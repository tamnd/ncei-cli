package ncei

import (
	"context"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes NOAA NCEI as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/ncei-cli/ncei"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// ncei:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone ncei binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the NCEI driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "ncei",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "ncei",
			Short:  "Query NOAA NCEI historical climate data",
			Long: `Query NOAA NCEI (National Centers for Environmental Information)
historical climate data over plain HTTPS. No API key required.

Fetch monthly or daily summaries for any GHCND station, or list the
built-in well-known stations. Output pipes cleanly into jq and other tools.`,
			Site: Host,
			Repo: "https://github.com/tamnd/ncei-cli",
		},
	}
}

// Register installs the client factory and every NCEI operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "monthly", Group: "read", List: true,
		Summary: "Monthly climate summary for a station",
	}, getMonthly)

	kit.Handle(app, kit.OpMeta{
		Name: "daily", Group: "read", List: true,
		Summary: "Daily climate summary for a station",
	}, getDaily)

	kit.Handle(app, kit.OpMeta{
		Name: "stations", Group: "read", List: true,
		Summary: "List well-known NCEI station IDs",
	}, getStations)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type monthlyInput struct {
	Station string  `kit:"flag" help:"station ID (e.g., USW00094728)"`
	Start   string  `kit:"flag" help:"start date (YYYY-MM or YYYY-MM-DD)"`
	End     string  `kit:"flag" help:"end date (YYYY-MM or YYYY-MM-DD)"`
	Limit   int     `kit:"flag,inherit" help:"max results"`
	Client  *Client `kit:"inject"`
}

type dailyInput struct {
	Station string  `kit:"flag" help:"station ID (e.g., USW00094728)"`
	Start   string  `kit:"flag" help:"start date (YYYY-MM-DD)"`
	End     string  `kit:"flag" help:"end date (YYYY-MM-DD)"`
	Limit   int     `kit:"flag,inherit" help:"max results"`
	Client  *Client `kit:"inject"`
}

type stationsInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getMonthly(ctx context.Context, in monthlyInput, emit func(MonthlyRecord) error) error {
	station, start, end := resolveDefaults(in.Station, in.Start, in.End)
	records, err := in.Client.GetMonthly(ctx, station, start, end, in.Limit)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func getDaily(ctx context.Context, in dailyInput, emit func(DailyRecord) error) error {
	station, start, end := resolveDefaults(in.Station, in.Start, in.End)
	records, err := in.Client.GetDaily(ctx, station, start, end, in.Limit)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func getStations(_ context.Context, _ stationsInput, emit func(KnownStation) error) error {
	for _, s := range KnownStations {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// Classify turns a station ID or keyword into a (type, id) pair.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty NCEI reference")
	}
	// Station IDs start with a country code followed by digits (e.g., USW00094728).
	if looksLikeStation(input) {
		return "station", input, nil
	}
	// Known query keywords.
	switch strings.ToLower(input) {
	case "monthly", "daily", "stations":
		return "query", strings.ToLower(input), nil
	}
	return "query", input, nil
}

// Locate is the inverse: the live NCEI CDO web URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "station":
		return "https://www.ncei.noaa.gov/cdo-web/datasets/GHCND/stations/GHCND:" + id + "/detail", nil
	case "query":
		return BaseURL + "?dataset=daily-summaries&stations=" + id, nil
	default:
		return "", errs.Usage("ncei has no resource type %q", uriType)
	}
}

// --- helpers ---

// resolveDefaults fills in empty station/start/end with sensible values.
func resolveDefaults(station, start, end string) (string, string, string) {
	if station == "" {
		station = "USW00094728" // Central Park NYC
	}
	now := time.Now()
	if end == "" {
		end = now.Format("2006-01-02")
	}
	if start == "" {
		start = now.AddDate(-1, 0, 0).Format("2006-01-02")
	}
	return station, start, end
}

// looksLikeStation returns true when the input looks like a GHCND station ID:
// starts with two or three uppercase ASCII letters followed by digits/letters.
func looksLikeStation(s string) bool {
	if len(s) < 6 {
		return false
	}
	for i, c := range s {
		if i < 2 {
			if c < 'A' || c > 'Z' {
				return false
			}
		} else {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return false
			}
		}
	}
	return true
}
