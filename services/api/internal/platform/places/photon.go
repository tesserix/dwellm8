package places

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Photon is the credential-free fallback. It indexes streets and places rather
// than door numbers, so what it is good for is landing somebody on the right
// locality with a real PIN, which they then refine by hand.
type Photon struct {
	URL    string
	Agent  string
	client *http.Client
}

func NewPhoton() *Photon {
	return &Photon{
		URL:    "https://photon.komoot.io/api",
		Agent:  "dwellm8/1.0 (+https://dwellm8.com)",
		client: &http.Client{Timeout: 4 * time.Second},
	}
}

type photonResponse struct {
	Features []struct {
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Name        string `json:"name"`
			Street      string `json:"street"`
			HouseNumber string `json:"housenumber"`
			District    string `json:"district"`
			City        string `json:"city"`
			County      string `json:"county"`
			State       string `json:"state"`
			Postcode    string `json:"postcode"`
			CountryCode string `json:"countrycode"`
		} `json:"properties"`
	} `json:"features"`
}

func (p *Photon) Suggest(ctx context.Context, q string) ([]Suggestion, error) {
	u, err := url.Parse(p.URL)
	if err != nil {
		return nil, fmt.Errorf("photon: bad endpoint: %w", err)
	}
	qs := u.Query()
	qs.Set("q", q)
	qs.Set("limit", "8")
	qs.Set("lang", "en")
	u.RawQuery = qs.Encode()

	var out photonResponse
	if err := p.get(ctx, u.String(), &out); err != nil {
		return nil, err
	}

	var found []Suggestion
	for _, f := range out.Features {
		pr := f.Properties
		// India only. A rental form that offers a London match is offering a
		// registered office nobody can file.
		if pr.CountryCode != "" && pr.CountryCode != "IN" {
			continue
		}
		code := StateCode(pr.State)
		if code == "" {
			continue
		}
		city := pr.City
		if city == "" {
			city = pr.County
		}
		s := Suggestion{
			Line1:     join(" ", pr.HouseNumber, join(", ", pr.Street, pr.Name)),
			Locality:  pr.District,
			City:      city,
			State:     pr.State,
			StateCode: code,
			Pin:       pr.Postcode,
		}
		if s.Line1 == "" {
			s.Line1 = pr.Name
		}
		s.Description = join(", ", s.Line1, s.Locality, s.City, s.State, s.Pin)
		if c := f.Geometry.Coordinates; len(c) == 2 {
			s.Lon, s.Lat = c[0], c[1]
		}
		found = append(found, s)
	}
	return found, nil
}

func (p *Photon) get(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("photon: building the request: %w", err)
	}
	req.Header.Set("User-Agent", p.Agent)
	res, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("photon: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("photon: returned %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		return fmt.Errorf("photon: decoding the response: %w", err)
	}
	return nil
}
