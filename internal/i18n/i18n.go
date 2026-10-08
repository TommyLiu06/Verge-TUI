// Package i18n reuses the pinned Clash Verge Rev translations.
package i18n

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"
)

var Languages = []string{"en", "ru", "zh", "fa", "tt", "id", "ar", "ko", "tr", "de", "es", "jp", "zhtw"}
var Names = map[string]string{"en": "English", "ru": "Русский", "zh": "简体中文", "fa": "فارسی", "tt": "Татар", "id": "Bahasa Indonesia", "ar": "العربية", "ko": "한국어", "tr": "Türkçe", "de": "Deutsch", "es": "Español", "jp": "日本語", "zhtw": "繁體中文"}

//go:embed catalogs/*.json
var resources embed.FS
var cache sync.Map

func catalog(language string) map[string]string {
	if value, ok := cache.Load(language); ok {
		return value.(map[string]string)
	}
	data, err := resources.ReadFile("catalogs/" + language + ".json")
	if err != nil {
		return nil
	}
	values := map[string]string{}
	if json.Unmarshal(data, &values) != nil {
		return nil
	}
	actual, _ := cache.LoadOrStore(language, values)
	return actual.(map[string]string)
}

func Text(language, english string) string {
	if language == "" {
		language = "en"
	}
	if value := catalog(language)[english]; value != "" {
		return value
	}
	if value := catalog("en")[english]; value != "" {
		return value
	}
	if head, tail, ok := strings.Cut(english, ": "); ok {
		if value := catalog(language)[head]; value != "" {
			return value + ": " + Text(language, tail)
		}
		if value := catalog("en")[head]; value != "" {
			return value + ": " + Text(language, tail)
		}
	}
	if strings.HasSuffix(english, ": ") {
		return Text(language, strings.TrimSuffix(english, ": ")) + ": "
	}
	return english
}
