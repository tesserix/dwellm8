package places

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Mappls is the primary provider where credentials exist: it is India-native
// and resolves flat and door numbers, which is the difference between a
// registered office somebody can be served notice at and an approximate street.
type Mappls struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	SearchURL    string

	client *http.Client
	mu     sync.Mutex
	token  string
	expiry time.Time
}

func NewMappls(id, secret string) *Mappls {
	return &Mappls{
		ClientID:     id,
		ClientSecret: secret,
		TokenURL:     "https://outpost.mappls.com/api/security/oauth/token",
		SearchURL:    "https://atlas.mappls.com/api/places/textsearch/json",
		client:       &http.Client{Timeout: 4 * time.Second},
	}
}

type mapplsResponse struct {
	SuggestedLocations []struct {
		PlaceName     string  `json:"placeName"`
		PlaceAddress  string  `json:"placeAddress"`
		Latitude      float64 `json:"latitude"`
		Longitude     float64 `json:"longitude"`
		AddressTokens struct {
			HouseNumber string `json:"houseNumber"`
			Street      string `json:"street"`
			SubLocality string `json:"subLocality"`
			Locality    string `json:"locality"`
			City        string `json:"city"`
			District    string `json:"district"`
			State       string `json:"state"`
			Pincode     string `json:"pincode"`
		} `json:"addressTokens"`
	} `json:"suggestedLocations"`
}

func (m *Mappls) Suggest(ctx context.Context, q string) ([]Suggestion, error) {
	token, err := m.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(m.SearchURL)
	if err != nil {
		return nil, fmt.Errorf("mappls: bad endpoint: %w", err)
	}
	qs := u.Query()
	qs.Set("query", q)
	qs.Set("region", "IND")
	u.RawQuery = qs.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("mappls: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mappls: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		m.forget()
		return nil, fmt.Errorf("mappls: the token was rejected")
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("mappls: returned %d", res.StatusCode)
	}

	var out mapplsResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("mappls: decoding the response: %w", err)
	}

	var found []Suggestion
	for _, l := range out.SuggestedLocations {
		t := l.AddressTokens
		locality := t.SubLocality
		if locality == "" {
			locality = t.Locality
		}
		city := t.City
		if city == "" {
			city = t.District
		}
		s := Suggestion{
			Description: join(", ", l.PlaceName, l.PlaceAddress),
			Line1:       join(" ", t.HouseNumber, join(", ", t.Street, l.PlaceName)),
			Locality:    locality,
			City:        city,
			State:       t.State,
			StateCode:   StateCode(t.State),
			Pin:         t.Pincode,
			Lat:         l.Latitude,
			Lon:         l.Longitude,
		}
		if s.Line1 == "" {
			s.Line1 = strings.TrimSpace(l.PlaceName)
		}
		found = append(found, s)
	}
	return found, nil
}

// accessToken caches the client-credentials token. It is good for a day, so
// fetching one per keystroke would be a token request per character typed.
func (m *Mappls) accessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token != "" && time.Now().Before(m.expiry) {
		return m.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {m.ClientID},
		"client_secret": {m.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("mappls: building the token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mappls: fetching a token: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", fmt.Errorf("mappls: the token endpoint returned %d", res.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("mappls: decoding the token: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("mappls: the token endpoint returned no token")
	}

	m.token = body.AccessToken
	// A minute of headroom, so a token is never spent on the request that
	// would have been its last.
	m.expiry = time.Now().Add(time.Duration(body.ExpiresIn)*time.Second - time.Minute)
	return m.token, nil
}

func (m *Mappls) forget() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token, m.expiry = "", time.Time{}
}
