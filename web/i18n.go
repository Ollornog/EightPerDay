// SPDX-License-Identifier: MIT

package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

//go:embed i18n/*.json
var catalogFiles embed.FS

// languages the interface speaks; the first one is the fallback.
var languages = []string{"en", "de"}

// catalog maps a language to its texts.
type catalog map[string]map[string]string

func loadCatalog() (catalog, error) {
	c := catalog{}
	for _, lang := range languages {
		raw, err := catalogFiles.ReadFile("i18n/" + lang + ".json")
		if err != nil {
			return nil, err
		}
		texts := map[string]string{}
		if err := json.Unmarshal(raw, &texts); err != nil {
			return nil, fmt.Errorf("i18n/%s.json: %w", lang, err)
		}
		c[lang] = texts
	}
	return c, nil
}

// translator returns the function a template calls as {{t "key"}} or {{t "key" arg ...}}. A missing key shows itself,
// so it is found in a test and not hidden.
func (c catalog) translator(lang string) func(key string, args ...any) string {
	texts := c[lang]
	return func(key string, args ...any) string {
		text, ok := texts[key]
		if !ok {
			return "!" + key
		}
		if len(args) > 0 {
			return fmt.Sprintf(text, args...)
		}
		return text
	}
}

// pickLanguage chooses the language of a request: the account's choice, then the cookie of the switch, then the
// browser's first language the interface knows.
func pickLanguage(r *http.Request, accountLanguage string) string {
	if known(accountLanguage) {
		return accountLanguage
	}
	if cookie, err := r.Cookie(languageCookie); err == nil && known(cookie.Value) {
		return cookie.Value
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if base, _, _ := strings.Cut(tag, "-"); known(base) {
			return base
		}
	}
	return languages[0]
}

func known(lang string) bool {
	for _, l := range languages {
		if l == lang {
			return true
		}
	}
	return false
}
