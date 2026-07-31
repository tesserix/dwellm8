// Package authz is the verb half of ADR-0020: may this principal perform this
// action on this object. Rows stay PostgreSQL's question (ADR-0003), and the
// two layers are deliberately redundant — a wrong answer here cannot widen
// what SQL will return.
//
// The model ships with the binary. model.json is generated from model.fga by
// `fga model transform` and CI fails if the two drift, so the model a store
// holds is always one this repository reviewed and tested.
package authz

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

//go:embed model.json
var modelJSON []byte

// Client speaks OpenFGA's HTTP API with the standard library, for the reason
// ADR-0027 §4 gives about the Firebase SDK: the calls are four JSON POSTs, and
// the SDK would be this service's second-heaviest dependency for them.
type Client struct {
	BaseURL string
	HTTP    *http.Client

	storeID string
	modelID string
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

// Bootstrap finds or creates the named store and ensures its latest
// authorization model is the embedded one. Models are immutable in OpenFGA, so
// "ensure" means: write a new version only when the latest differs, and pin
// every subsequent check to the id that matched. A rollback is a deploy of the
// previous binary, which writes (or re-finds) the previous model the same way.
func (c *Client) Bootstrap(ctx context.Context, storeName string) error {
	id, err := c.findStore(ctx, storeName)
	if err != nil {
		return err
	}
	if id == "" {
		if id, err = c.createStore(ctx, storeName); err != nil {
			return err
		}
	}
	c.storeID = id

	modelID, err := c.ensureModel(ctx)
	if err != nil {
		return err
	}
	c.modelID = modelID
	return nil
}

func (c *Client) findStore(ctx context.Context, name string) (string, error) {
	var out struct {
		Stores []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"stores"`
		ContinuationToken string `json:"continuation_token"`
	}
	token := ""
	for {
		u := c.BaseURL + "/stores?page_size=50"
		if token != "" {
			u += "&continuation_token=" + url.QueryEscape(token)
		}
		if err := c.do(ctx, http.MethodGet, u, nil, &out); err != nil {
			return "", fmt.Errorf("list stores: %w", err)
		}
		for _, s := range out.Stores {
			if s.Name == name {
				return s.ID, nil
			}
		}
		if out.ContinuationToken == "" {
			return "", nil
		}
		token = out.ContinuationToken
	}
}

func (c *Client) createStore(ctx context.Context, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]string{"name": name}
	if err := c.do(ctx, http.MethodPost, c.BaseURL+"/stores", body, &out); err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}
	return out.ID, nil
}

// ensureModel compares the latest stored model against the embedded one with
// the ids stripped, both round-tripped through the same decoder so field order
// cannot manufacture a difference. Equal means reuse; different means write.
func (c *Client) ensureModel(ctx context.Context) (string, error) {
	var latest struct {
		Models []json.RawMessage `json:"authorization_models"`
	}
	u := c.BaseURL + "/stores/" + c.storeID + "/authorization-models?page_size=1"
	if err := c.do(ctx, http.MethodGet, u, nil, &latest); err != nil {
		return "", fmt.Errorf("read latest model: %w", err)
	}
	if len(latest.Models) == 1 {
		var stored, shipped map[string]any
		if err := json.Unmarshal(latest.Models[0], &stored); err != nil {
			return "", fmt.Errorf("decode stored model: %w", err)
		}
		if err := json.Unmarshal(modelJSON, &shipped); err != nil {
			return "", fmt.Errorf("decode embedded model: %w", err)
		}
		id, _ := stored["id"].(string)
		delete(stored, "id")
		delete(shipped, "id")
		if equalJSON(stored, shipped) {
			return id, nil
		}
	}

	var out struct {
		ID string `json:"authorization_model_id"`
	}
	err := c.do(ctx, http.MethodPost,
		c.BaseURL+"/stores/"+c.storeID+"/authorization-models", json.RawMessage(modelJSON), &out)
	if err != nil {
		return "", fmt.Errorf("write model: %w", err)
	}
	return out.ID, nil
}

// Check answers one tuple. The evaluation clock is this server's, passed as
// context, never the client's — conditions like mandate_active are security
// decisions and the requester does not get to pick the time they are asked at.
func (c *Client) Check(ctx context.Context, user, relation, object string) (bool, error) {
	if c.storeID == "" || c.modelID == "" {
		return false, fmt.Errorf("authz: check before bootstrap")
	}
	body := map[string]any{
		"authorization_model_id": c.modelID,
		"tuple_key": map[string]string{
			"user":     user,
			"relation": relation,
			"object":   object,
		},
		"context": map[string]string{"now": time.Now().UTC().Format(time.RFC3339)},
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := c.do(ctx, http.MethodPost, c.BaseURL+"/stores/"+c.storeID+"/check", body, &out); err != nil {
		return false, err
	}
	return out.Allowed, nil
}

func (c *Client) do(ctx context.Context, method, u string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return fmt.Errorf("openfga %s: %s %s", resp.Status, apiErr.Code, apiErr.Message)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// equalJSON compares two decoded documents by re-encoding them: encoding/json
// sorts map keys, so field order cannot manufacture a difference. A spurious
// mismatch is harmless — models are immutable, and writing an identical one
// only mints a new id.
func equalJSON(a, b map[string]any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}
