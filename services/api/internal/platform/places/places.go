// Package places turns what somebody types into an address the rest of the
// system can act on — a registered office with a PIN and a state code, because
// the state is what decides RERA jurisdiction and the GST place of supply.
//
// Two providers, in order: Mappls (MapmyIndia) when credentials are set, since
// it is India-native and indexes flats and door numbers; Photon (OpenStreetMap)
// otherwise, which needs no key but only knows streets and places. Same order
// and same reasoning as HomeChef's address picker.
package places

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// MinQuery is short enough to be useful and long enough that a paid geocoder is
// not billed for somebody's first two keystrokes.
const MinQuery = 3

// ErrUnavailable separates "every geocoder we tried is down" from "no such
// place" — the first is worth a retry, the second is not.
var ErrUnavailable = errors.New("address lookup is unavailable")

// Suggestion is one match, already split into the fields a registration form
// holds so the client never has to parse a formatted address back apart.
type Suggestion struct {
	Description string  `json:"description"`
	Line1       string  `json:"line1"`
	Locality    string  `json:"locality,omitempty"`
	City        string  `json:"city,omitempty"`
	StateCode   string  `json:"state_code,omitempty"`
	State       string  `json:"state,omitempty"`
	Pin         string  `json:"pin_code,omitempty"`
	Lat         float64 `json:"lat,omitempty"`
	Lon         float64 `json:"lon,omitempty"`
}

// Provider is one geocoder.
type Provider interface {
	Suggest(ctx context.Context, q string) ([]Suggestion, error)
}

// Config carries the Mappls credentials, read from the environment which reads
// them from Secret Manager — never from a file in the image.
type Config struct {
	MapplsClientID     string
	MapplsClientSecret string
}

// Chain tries its providers in order and returns the first real answer.
type Chain struct {
	providers []Provider
	log       *slog.Logger
}

func NewChain(log *slog.Logger, p ...Provider) *Chain {
	return &Chain{providers: p, log: log}
}

// New assembles the house chain: Mappls first when it is paid for, Photon
// always, so a missing credential degrades instead of breaking the form.
func New(cfg Config, log *slog.Logger) *Chain {
	var ps []Provider
	if cfg.MapplsClientID != "" && cfg.MapplsClientSecret != "" {
		ps = append(ps, NewMappls(cfg.MapplsClientID, cfg.MapplsClientSecret))
	}
	return NewChain(log, append(ps, NewPhoton())...)
}

func (c *Chain) Suggest(ctx context.Context, q string) ([]Suggestion, error) {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) < MinQuery {
		return nil, nil
	}
	tried, failed := 0, 0
	for _, p := range c.providers {
		tried++
		out, err := p.Suggest(ctx, q)
		if err != nil {
			failed++
			// The query is somebody's home address, so it is not logged.
			c.log.Warn("address lookup provider failed", "error", err)
			continue
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if tried > 0 && tried == failed {
		return nil, ErrUnavailable
	}
	return nil, nil
}

// StateCode resolves a state's name to the two letters the statutory forms
// carry. Empty when it is not an Indian subdivision, which is also how a
// foreign match gets discarded.
func StateCode(name string) string {
	n := normaliseState(name)
	if canonical, ok := stateAliases[n]; ok {
		n = canonical
	}
	return stateCodes[n]
}

func normaliseState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "state of ")
	s = strings.TrimPrefix(s, "nct of ")
	s = strings.ReplaceAll(s, "&", "and")
	return strings.Join(strings.Fields(s), " ")
}

// The ISO 3166-2:IN letters, which are also what RERA and GST registrations
// print. Keyed on the normalised name, with the aliases OSM and Mappls
// actually return alongside the official spelling.
var stateCodes = map[string]string{
	"andhra pradesh": "AP", "arunachal pradesh": "AR", "assam": "AS", "bihar": "BR",
	"chhattisgarh": "CG", "goa": "GA", "gujarat": "GJ", "haryana": "HR",
	"himachal pradesh": "HP", "jharkhand": "JH", "karnataka": "KA", "kerala": "KL",
	"madhya pradesh": "MP", "maharashtra": "MH", "manipur": "MN", "meghalaya": "ML",
	"mizoram": "MZ", "nagaland": "NL", "odisha": "OD", "punjab": "PB",
	"rajasthan": "RJ", "sikkim": "SK", "tamil nadu": "TN", "telangana": "TG",
	"tripura": "TR", "uttar pradesh": "UP", "uttarakhand": "UK", "west bengal": "WB",
	"andaman and nicobar islands": "AN", "chandigarh": "CH",
	"dadra and nagar haveli and daman and diu": "DH", "delhi": "DL",
	"jammu and kashmir": "JK", "ladakh": "LA", "lakshadweep": "LD", "puducherry": "PY",
}

// What OSM and Mappls actually return where it differs from the official name.
var stateAliases = map[string]string{
	"orissa": "odisha", "pondicherry": "puducherry", "uttaranchal": "uttarakhand",
	"national capital territory of delhi": "delhi", "new delhi": "delhi",
}

// join builds a line1 out of whatever parts a provider gave, without leaving
// stray commas where a field was missing.
func join(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
