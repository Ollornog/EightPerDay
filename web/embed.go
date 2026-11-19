// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/Ollornog/EightPerDay/web/auth"
)

// Embedding runs the interface inside another application (M-3), such as UnderDoc: under a path of its own, with the
// application's sign-in and the tenant of its request, in a frame of its pages. The application mounts the handler at
// Base, makes sure only signed-in people of the right tenant reach it, and passes a store whose connections carry
// that tenant. Then the program's own sign-in, accounts and administration are gone: no login, setup, account or
// admin page, no organization founded here (the application maps its tenant to an organization itself).
type Embedding struct {
	// Base is the path the application mounts the interface at, such as "/time": every link and every redirect
	// carries it. Empty is the root.
	Base string
	// Account names who asks, from the application's own sign-in: its ID is the person in the store. Language, when
	// set, is the language of the pages. Nil means nobody is signed in; such a request gets 401.
	Account func(r *http.Request) *auth.Account
	// Directory lists the accounts of the application: the names of members, and whom an organization can take in.
	Directory Directory
	// ThemeKey is the key in the browser's localStorage under which the application keeps its theme ("light",
	// "dark" or "system"), so the frame follows it; empty is C22's own key, themeMode. The pages then show no theme
	// switch of their own.
	ThemeKey string
}

// Directory names accounts: the standalone program's own (auth.Service) or those of an embedding application.
type Directory interface {
	// Account returns one account by its ID.
	Account(ctx context.Context, id string) (*auth.Account, error)
	// Accounts returns every account.
	Accounts(ctx context.Context) ([]*auth.Account, error)
}

var _ Directory = (*auth.Service)(nil)

// embedded reports whether the handler runs inside another application.
func (h *handler) embedded() bool { return h.options.Embedding != nil }

// base is the path in front of every link: the mount path of an embedding application, or nothing.
func (h *handler) base() string {
	if h.options.Embedding == nil {
		return ""
	}
	return h.options.Embedding.Base
}

func (h *handler) directory() Directory {
	if h.options.Embedding != nil {
		return h.options.Embedding.Directory
	}
	return h.options.Auth
}

// secureCookies: cookies only over https, as the address people open says. Embedded without an origin, the request
// decides: TLS here or at the proxy in front.
func (h *handler) secureCookies(r *http.Request) bool {
	if h.options.Auth != nil && !h.embedded() {
		return h.options.Auth.SecureCookies()
	}
	if h.options.Origin != nil {
		return h.options.Origin.Scheme == "https"
	}
	return r != nil && (r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}

// mounted serves the handler below base: the request loses the prefix, and a redirect to a path of the interface gets
// it in front again, so the pages and their code only ever know paths from the root of the interface.
func mounted(base string, next http.Handler) http.Handler {
	if base == "" {
		return next
	}
	strip := http.StripPrefix(base, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == base {
			target := base + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		if !strings.HasPrefix(r.URL.Path, base+"/") {
			http.NotFound(w, r)
			return
		}
		strip.ServeHTTP(&baseWriter{ResponseWriter: w, base: base}, r)
	})
}

// baseWriter puts the mount path in front of a redirect within the interface.
type baseWriter struct {
	http.ResponseWriter
	base  string
	wrote bool
}

func (b *baseWriter) WriteHeader(code int) {
	if !b.wrote {
		b.wrote = true
		if loc := b.Header().Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") &&
			!strings.HasPrefix(loc, "/\\") {
			b.Header().Set("Location", b.base+loc)
		}
	}
	b.ResponseWriter.WriteHeader(code)
}

func (b *baseWriter) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(http.StatusOK)
	}
	return b.ResponseWriter.Write(p)
}

func (b *baseWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }
