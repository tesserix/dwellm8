package places

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const photonBody = `{"features":[
  {"geometry":{"coordinates":[76.2999,9.9674]},
   "properties":{"name":"Chandra Arcade","street":"Kadavanthra Road","housenumber":"12",
                 "district":"Kadavanthra","city":"Kochi","state":"Kerala",
                 "postcode":"682020","countrycode":"IN"}},
  {"geometry":{"coordinates":[-0.12,51.5]},
   "properties":{"name":"Kochi Restaurant","city":"London","state":"England",
                 "postcode":"WC2N","countrycode":"GB"}}]}`

const mapplsBody = `{"suggestedLocations":[
  {"placeName":"Chandra Arcade","placeAddress":"Kadavanthra, Kochi, Kerala, 682020",
   "latitude":9.9674,"longitude":76.2999,
   "addressTokens":{"houseNumber":"12","street":"Kadavanthra Road","subLocality":"Kadavanthra",
                    "city":"Kochi","state":"Kerala","pincode":"682020"}}]}`

func photonServer(t *testing.T, body string) *Photon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Errorf("photon called without a query")
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	p := NewPhoton()
	p.URL = srv.URL
	return p
}

func TestPhotonReturnsIndianMatchesWithTheStateResolvedToItsCode(t *testing.T) {
	out, err := photonServer(t, photonBody).Suggest(context.Background(), "Chandra Arcade Kochi")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want the one Indian match, got %d: %+v", len(out), out)
	}
	got := out[0]
	if got.Line1 != "12 Kadavanthra Road, Chandra Arcade" || got.City != "Kochi" || got.Pin != "682020" {
		t.Errorf("address parsed wrong: %+v", got)
	}
	if got.StateCode != "KL" {
		t.Errorf("Kerala should resolve to KL, got %q", got.StateCode)
	}
	if got.Lat == 0 || got.Lon == 0 {
		t.Errorf("coordinates dropped: %+v", got)
	}
}

func TestForeignMatchesAreDiscarded(t *testing.T) {
	out, _ := photonServer(t, photonBody).Suggest(context.Background(), "Kochi")
	for _, s := range out {
		if s.City == "London" {
			t.Fatalf("a London match reached an Indian rental form: %+v", s)
		}
	}
}

func TestMapplsAnswersFromItsAddressTokens(t *testing.T) {
	var tokens int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			atomic.AddInt32(&tokens, 1)
			_, _ = io.WriteString(w, `{"access_token":"tok","expires_in":86400}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("search sent without the token: %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, mapplsBody)
	}))
	t.Cleanup(srv.Close)

	m := NewMappls("id", "secret")
	m.TokenURL, m.SearchURL = srv.URL+"/token", srv.URL+"/search"

	for i := 0; i < 3; i++ {
		out, err := m.Suggest(context.Background(), "Chandra Arcade Kochi")
		if err != nil {
			t.Fatalf("suggest: %v", err)
		}
		if len(out) != 1 || out[0].StateCode != "KL" || out[0].Locality != "Kadavanthra" {
			t.Fatalf("mappls parsed wrong: %+v", out)
		}
	}
	// A token good for a day must not be fetched once per keystroke.
	if n := atomic.LoadInt32(&tokens); n != 1 {
		t.Errorf("want one token fetch for three searches, got %d", n)
	}
}

type stub struct {
	out  []Suggestion
	err  error
	hits int32
}

func (s *stub) Suggest(context.Context, string) ([]Suggestion, error) {
	atomic.AddInt32(&s.hits, 1)
	return s.out, s.err
}

func TestTheFirstProviderThatAnswersWins(t *testing.T) {
	first := &stub{out: []Suggestion{{City: "Kochi"}}}
	second := &stub{out: []Suggestion{{City: "Nowhere"}}}
	out, err := NewChain(quiet(), first, second).Suggest(context.Background(), "kochi")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(out) != 1 || out[0].City != "Kochi" {
		t.Fatalf("wrong provider answered: %+v", out)
	}
	if second.hits != 0 {
		t.Errorf("the fallback ran even though the primary answered")
	}
}

func TestAFailingProviderFallsThroughToTheNext(t *testing.T) {
	broken := &stub{err: errors.New("credential expired")}
	good := &stub{out: []Suggestion{{City: "Kochi"}}}
	out, err := NewChain(quiet(), broken, good).Suggest(context.Background(), "kochi")
	if err != nil || len(out) != 1 {
		t.Fatalf("want the fallback's answer, got %+v / %v", out, err)
	}
}

func TestEveryProviderFailingReadsAsAnOutageNotAnEmptyResult(t *testing.T) {
	// The distinction the client needs: "try again" versus "no such place".
	chain := NewChain(quiet(), &stub{err: errors.New("down")}, &stub{err: errors.New("down")})
	if _, err := chain.Suggest(context.Background(), "kochi"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestAProviderWithNothingToSayIsNotAnOutage(t *testing.T) {
	out, err := NewChain(quiet(), &stub{}).Suggest(context.Background(), "zzzzzz")
	if err != nil || len(out) != 0 {
		t.Fatalf("want an empty answer, got %+v / %v", out, err)
	}
}

func TestShortQueriesReachNoProvider(t *testing.T) {
	s := &stub{out: []Suggestion{{City: "Kochi"}}}
	out, err := NewChain(quiet(), s).Suggest(context.Background(), "ko")
	if err != nil || len(out) != 0 {
		t.Fatalf("want nothing for a two-letter query, got %+v / %v", out, err)
	}
	if s.hits != 0 {
		t.Errorf("a two-letter query was sent to a paid geocoder")
	}
}

func TestMapplsIsOnlyUsedWhenItsCredentialsAreSet(t *testing.T) {
	if got := len(New(Config{}, quiet()).providers); got != 1 {
		t.Errorf("with no credentials only Photon should be in the chain, got %d", got)
	}
	if got := len(New(Config{MapplsClientID: "i", MapplsClientSecret: "s"}, quiet()).providers); got != 2 {
		t.Errorf("with credentials both providers should be in the chain, got %d", got)
	}
}

func TestStateCodesCoverEveryStateAndUnionTerritory(t *testing.T) {
	// 28 states + 8 union territories. A missing one silently leaves the
	// registration form's state field blank for everybody living there.
	if len(stateCodes) != 36 {
		t.Errorf("want 36 subdivisions, got %d", len(stateCodes))
	}
	for _, c := range []struct{ name, code string }{
		{"Kerala", "KL"}, {"Tamil Nadu", "TN"}, {"NCT of Delhi", "DL"},
		{"Odisha", "OD"}, {"Uttar Pradesh", "UP"}, {"Jammu and Kashmir", "JK"},
	} {
		if got := StateCode(c.name); got != c.code {
			t.Errorf("StateCode(%q) = %q, want %q", c.name, got, c.code)
		}
	}
	if got := StateCode("Bavaria"); got != "" {
		t.Errorf("an unknown state should resolve to nothing, got %q", got)
	}
}
