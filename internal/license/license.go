// Package license gates Pro features with a Gumroad license key and a
// 14-day local trial. The core CLI never phones home; only `pf license
// activate` contacts Gumroad's license verification endpoint.
package license

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GumroadProductID is set at release build time:
//
//	go build -ldflags "-X github.com/aaronalphalabs-ship-it/prompt-factory/internal/license.GumroadProductID=XXXX"
var GumroadProductID = ""

const TrialDays = 14

type State struct {
	Key        string    `json:"key,omitempty"`
	Email      string    `json:"email,omitempty"`
	Tier       string    `json:"tier,omitempty"`
	VerifiedAt time.Time `json:"verified_at,omitempty"`
	TrialStart time.Time `json:"trial_start,omitempty"`
}

func path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "prompt-factory", "license.json")
}

func Load() *State {
	s := &State{}
	if b, err := os.ReadFile(path()); err == nil {
		_ = json.Unmarshal(b, s)
	}
	return s
}

func (s *State) Save() error {
	p := path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// Require returns nil when Pro is usable (licensed or in trial) and starts
// the trial on first use.
func Require(feature string) error {
	if os.Getenv("PF_PRO_KEY") != "" { // CI / team machines
		return nil
	}
	s := Load()
	if s.Key != "" {
		return nil
	}
	if s.TrialStart.IsZero() {
		s.TrialStart = time.Now()
		_ = s.Save()
		fmt.Fprintf(os.Stderr, "✦ `%s` is a Pro feature. Your %d-day free trial starts now.\n", feature, TrialDays)
		return nil
	}
	left := TrialDays - int(time.Since(s.TrialStart).Hours()/24)
	if left > 0 {
		fmt.Fprintf(os.Stderr, "✦ Pro trial: %d day(s) left. `pf license activate <key>` to keep it.\n", left)
		return nil
	}
	return fmt.Errorf("`%s` is a Pro feature and your trial has ended.\n  Get a key: https://aaronalpha.top/pf\n  Then run:  pf license activate <key>", feature)
}

func (s *State) Status() string {
	switch {
	case s.Key != "":
		return fmt.Sprintf("Pro (%s) — activated %s", nz(s.Tier, "licensed"), s.VerifiedAt.Format("2006-01-02"))
	case s.TrialStart.IsZero():
		return "Free — Pro trial not started"
	default:
		left := TrialDays - int(time.Since(s.TrialStart).Hours()/24)
		if left <= 0 {
			return "Free — Pro trial ended"
		}
		return fmt.Sprintf("Pro trial — %d day(s) left", left)
	}
}

// Activate verifies a key against Gumroad and stores it.
func Activate(ctx context.Context, key string) (*State, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("empty license key")
	}
	if GumroadProductID == "" {
		return nil, errors.New("this build has no Gumroad product configured; download an official release from https://aaronalpha.top/pf")
	}
	form := url.Values{
		"product_id":           {GumroadProductID},
		"license_key":          {key},
		"increment_uses_count": {"true"},
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.gumroad.com/v2/licenses/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		Purchase struct {
			Email                   string `json:"email"`
			Refunded                bool   `json:"refunded"`
			Chargebacked            bool   `json:"chargebacked"`
			SubscriptionCancelledAt string `json:"subscription_cancelled_at"`
			SubscriptionFailedAt    string `json:"subscription_failed_at"`
			Variants                string `json:"variants"`
		} `json:"purchase"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("unexpected response from Gumroad (HTTP %d)", resp.StatusCode)
	}
	if !out.Success {
		return nil, fmt.Errorf("license not valid: %s", nz(out.Message, "unknown error"))
	}
	p := out.Purchase
	if p.Refunded || p.Chargebacked || p.SubscriptionCancelledAt != "" || p.SubscriptionFailedAt != "" {
		return nil, errors.New("this license is no longer active (refunded, cancelled or payment failed)")
	}
	s := Load()
	s.Key, s.Email, s.Tier, s.VerifiedAt = key, p.Email, p.Variants, time.Now()
	return s, s.Save()
}

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
