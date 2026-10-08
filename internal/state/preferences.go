package state

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"verge-tui/internal/i18n"
)

var Themes = []string{"terminal", "dark", "light", "mist-dark", "mist-light", "modern-dark", "modern-light", "vivid-dark", "vivid-light"}

// Preferences belong to the calling user, independently of shared core settings.
type Preferences struct {
	Language     string `json:"language"`
	Theme        string `json:"theme"`
	StartPage    string `json:"start_page"`
	ProxyColumns int    `json:"proxy_columns"`
	DelayURL     string `json:"delay_url"`
	DelayTimeout int    `json:"delay_timeout_ms"`
}

func DefaultPreferences() Preferences {
	return Preferences{Language: "en", Theme: "terminal", StartPage: "home", DelayTimeout: 5000}
}

func (p Preferences) Validate() error {
	if !slices.Contains(i18n.Languages, p.Language) {
		return errors.New("unsupported language")
	}
	if !slices.Contains(Themes, p.Theme) {
		return errors.New("invalid theme")
	}
	if p.StartPage != "home" && p.StartPage != "proxies" && p.StartPage != "profiles" && p.StartPage != "settings" {
		return errors.New("invalid start page")
	}
	if p.ProxyColumns < 0 || p.ProxyColumns > 5 {
		return errors.New("proxy columns must be Auto or between 1 and 5")
	}
	if p.DelayTimeout < 100 || p.DelayTimeout > 60000 {
		return errors.New("delay timeout must be between 100 and 60000 ms")
	}
	if p.DelayURL != "" {
		u, err := url.Parse(p.DelayURL)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("enter an HTTP or HTTPS delay test URL")
		}
	}
	return nil
}

func ReadPreferences(root string) (Preferences, error) {
	p := DefaultPreferences()
	data, err := os.ReadFile(filepath.Join(root, "preferences.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}

func WritePreferences(root string, p Preferences) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return Write(filepath.Join(root, "preferences.json"), append(data, '\n'), 0600)
}
