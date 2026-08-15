package onboarding

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BackgroundProvider is a vendor-neutral HTTP adapter. Checkr, Sterling, or a
// regional provider can be configured without embedding vendor secrets in code.
type BackgroundProvider struct {
	name, apiURL, apiKey, webhookSecret string
	client                              *http.Client
}

type BackgroundProviderResult struct {
	ReferenceID string `json:"id"`
	Status      string `json:"status"`
}
type BackgroundWebhook struct {
	EventID     string `json:"event_id"`
	ReferenceID string `json:"reference_id"`
	Status      string `json:"status"`
	ReportURL   string `json:"report_url"`
	Notes       string `json:"notes"`
}

func NewBackgroundProvider(name, apiURL, apiKey, webhookSecret string) *BackgroundProvider {
	return &BackgroundProvider{name: name, apiURL: apiURL, apiKey: apiKey, webhookSecret: webhookSecret, client: &http.Client{Timeout: 15 * time.Second}}
}
func (p *BackgroundProvider) Configured() bool {
	return p != nil && p.name != "" && p.apiURL != "" && p.apiKey != "" && p.webhookSecret != ""
}
func (p *BackgroundProvider) Name() string { return p.name }

func (p *BackgroundProvider) Start(ctx context.Context, driver *DriverInfo) (*BackgroundProviderResult, error) {
	body, _ := json.Marshal(map[string]interface{}{"candidate_id": driver.ID, "email": driver.Email, "first_name": driver.FirstName, "last_name": driver.LastName, "phone": driver.PhoneNumber})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("background provider request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("background provider HTTP %d: %s", resp.StatusCode, string(limited))
	}
	result := &BackgroundProviderResult{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}
	if result.ReferenceID == "" {
		return nil, fmt.Errorf("background provider returned no reference id")
	}
	return result, nil
}

func (p *BackgroundProvider) Verify(payload []byte, signature string) bool {
	signature = strings.TrimPrefix(signature, "sha256=")
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(p.webhookSecret))
	_, _ = mac.Write(payload)
	return hmac.Equal(decoded, mac.Sum(nil))
}
