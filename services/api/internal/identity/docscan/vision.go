package docscan

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Vision is Cloud Vision's document text recognition. DOCUMENT_TEXT_DETECTION
// rather than TEXT_DETECTION: it is the dense-text model, and a machine-readable
// zone is dense text whose line structure has to survive.
type Vision struct {
	Endpoint string
	Token    func(ctx context.Context) (string, error)

	client *http.Client
}

func NewVision(token func(ctx context.Context) (string, error)) *Vision {
	return &Vision{
		Endpoint: "https://vision.googleapis.com/v1/images:annotate",
		Token:    token,
		// A document scan is interactive: the manager is holding the card.
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type visionRequest struct {
	Requests []visionAnnotate `json:"requests"`
}

type visionAnnotate struct {
	Image        visionImage     `json:"image"`
	Features     []visionFeature `json:"features"`
	ImageContext visionContext   `json:"imageContext"`
}

type visionImage struct {
	Content string `json:"content"`
}

type visionFeature struct {
	Type string `json:"type"`
}

type visionContext struct {
	LanguageHints []string `json:"languageHints"`
}

type visionResponse struct {
	Responses []struct {
		FullTextAnnotation struct {
			Text string `json:"text"`
		} `json:"fullTextAnnotation"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"responses"`
}

func (v *Vision) Text(ctx context.Context, image []byte) (string, error) {
	body, err := json.Marshal(visionRequest{Requests: []visionAnnotate{{
		Image:    visionImage{Content: base64.StdEncoding.EncodeToString(image)},
		Features: []visionFeature{{Type: "DOCUMENT_TEXT_DETECTION"}},
		// A PAN card is printed in Devanagari beside English, and the hint keeps
		// the English lines from being scored against the wrong script.
		ImageContext: visionContext{LanguageHints: []string{"en", "hi"}},
	}}})
	if err != nil {
		return "", fmt.Errorf("docscan: encoding the vision request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("docscan: building the vision request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// The credential travels in the header, never the query string: a URL is
	// logged by every hop and this one carries an identity document behind it.
	token, err := v.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("docscan: no vision credential: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("docscan: calling vision: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("docscan: reading the vision response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docscan: vision answered %d", resp.StatusCode)
	}

	var out visionResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", fmt.Errorf("docscan: decoding the vision response: %w", err)
	}
	if len(out.Responses) == 0 {
		return "", fmt.Errorf("docscan: vision returned no annotation")
	}
	if msg := out.Responses[0].Error.Message; msg != "" {
		return "", fmt.Errorf("docscan: vision refused the image: %s", msg)
	}
	return out.Responses[0].FullTextAnnotation.Text, nil
}
