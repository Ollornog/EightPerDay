// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/web/auth"
)

// employerCookie remembers the employer a person looked at last, per browser.
const employerCookie = "epd_employer"

// The interface reads and writes time records only through the core library: every rule of calculation lives there
// (ADR-1). What follows is the glue: which employer and week a page shows, who acts, and the forms' input.

func (h *handler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now()
}

// today is the date of an employer's time zone, or of the default zone without one.
func (h *handler) today(emp *core.Employer) core.Date {
	loc, err := time.LoadLocation(core.DefaultTimeZone)
	if emp != nil {
		if l, e := emp.Location(); e == nil {
			loc, err = l, nil
		}
	}
	if err != nil {
		loc = time.UTC
	}
	return core.DateIn(h.now(), loc)
}

// catalog loads the catalog of the signed-in person.
func (h *handler) ownCatalog(ctx context.Context) (core.Catalog, error) {
	return h.options.Store.Catalog(ctx, accountFrom(ctx).ID)
}

// activeEmployers are the employers that are not archived, private first, then by name.
func activeEmployers(cat core.Catalog) []core.Employer {
	var out []core.Employer
	for _, e := range cat.Employers {
		if !e.Archived {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b core.Employer) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

// pickEmployer chooses the employer of a page: the one the address names, the one looked at last, the first one.
func (h *handler) pickEmployer(w http.ResponseWriter, r *http.Request, cat core.Catalog) (*core.Employer, bool) {
	employers := activeEmployers(cat)
	if len(employers) == 0 {
		return nil, false
	}
	want := r.URL.Query().Get("e")
	if want == "" {
		if c, err := r.Cookie(employerCookie); err == nil {
			want = c.Value
		}
	}
	for i := range employers {
		if employers[i].ID == want {
			h.rememberEmployer(w, want)
			return &employers[i], true
		}
	}
	return &employers[0], true
}

func (h *handler) rememberEmployer(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: employerCookie, Value: id, Path: "/", MaxAge: 365 * 24 * 3600, HttpOnly: true,
		Secure: h.options.Auth.SecureCookies(), SameSite: http.SameSiteLaxMode})
}

// formEmployer returns the employer a form names, if it is the person's own and may be booked on.
func (h *handler) formEmployer(r *http.Request, cat core.Catalog) (core.Employer, error) {
	emp, ok := cat.Employer(r.PostFormValue("employer"))
	if !ok || emp.Archived {
		return core.Employer{}, core.ErrNotFound
	}
	if !core.MayBook(emp, core.Actor{Person: accountFrom(r.Context()).ID}) {
		return core.Employer{}, errForbidden
	}
	return emp, nil
}

var errForbidden = errors.New("forbidden")

// weekOf returns the first day of the week the address names (?w=2026-11-16), or of the current one.
func weekOf(r *http.Request, emp core.Employer, today core.Date) core.Date {
	day := today
	if raw := r.URL.Query().Get("w"); raw != "" {
		if d, err := core.ParseDate(raw); err == nil {
			day = d
		}
	}
	return day.StartOfWeek(emp.WeekStart)
}

// formDate reads a date field.
func formDate(r *http.Request, name string) (core.Date, error) {
	return core.ParseDate(strings.TrimSpace(r.PostFormValue(name)))
}

var (
	clockDuration = regexp.MustCompile(`^(\d{1,3}):([0-5]\d)$`)
	unitDuration  = regexp.MustCompile(`^(?:(\d{1,3})\s*h)?\s*(?:(\d{1,4})\s*(?:m|min))?$`)
	decimalHours  = regexp.MustCompile(`^(\d{1,3})(?:[.,](\d{1,2}))?$`)
)

// durationPattern is the pattern attribute of a duration field (C22 block data-entry/duration-field): the forms
// parseHours accepts, so the browser says early that a value is not a duration. The server still decides. Written for
// the browser's v-mode and for Go's regexp alike; a test keeps it in step with parseHours.
const durationPattern = `\s*(?:-\s*|\+)?(?:\d{1,3}:[0-5]\d|\d{1,3}(?:[.,]\d{1,2})?|(?:\d{1,3}\s*[hH])?\s*\d{1,4}\s*(?:[mM]|[mM][iI][nN])|\d{1,3}\s*[hH])\s*`

// parseHours reads a duration as people type it into a field: 38:30, 38,5, 38.5, 38h30m, 38h, 90m. Empty is zero.
// It is input handling only; what the minutes mean is the core library's business.
func parseHours(input string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	sign := 1
	if strings.HasPrefix(s, "-") {
		sign, s = -1, strings.TrimSpace(s[1:])
	} else {
		s = strings.TrimPrefix(s, "+")
	}
	if s == "" {
		return 0, nil
	}
	if m := clockDuration.FindStringSubmatch(s); m != nil {
		hours, _ := strconv.Atoi(m[1])
		minutes, _ := strconv.Atoi(m[2])
		return sign * (hours*60 + minutes), nil
	}
	if m := decimalHours.FindStringSubmatch(s); m != nil {
		hours, _ := strconv.Atoi(m[1])
		fraction := 0
		if m[2] != "" {
			digits := m[2]
			if len(digits) == 1 {
				digits += "0"
			}
			hundredths, _ := strconv.Atoi(digits)
			fraction = (hundredths*60 + 50) / 100
		}
		return sign * (hours*60 + fraction), nil
	}
	if m := unitDuration.FindStringSubmatch(s); m != nil && (m[1] != "" || m[2] != "") {
		hours, _ := strconv.Atoi(m[1])
		minutes, _ := strconv.Atoi(m[2])
		return sign * (hours*60 + minutes), nil
	}
	return 0, fmt.Errorf("not a duration: %q", input)
}

// parseClock reads a time of day such as 8, 8:00 or 16:30 as minutes after midnight.
func parseClock(input string) (int, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, false
	}
	hours, minutes := s, "0"
	if h, m, ok := strings.Cut(s, ":"); ok {
		hours, minutes = h, m
	}
	hh, err1 := strconv.Atoi(hours)
	mm, err2 := strconv.Atoi(minutes)
	if err1 != nil || err2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 || hh*60+mm > 24*60 {
		return 0, false
	}
	return hh*60 + mm, true
}

// change is who changes a time record, and why.
func change(r *http.Request, reason string) core.Change {
	return core.Change{By: accountFrom(r.Context()).ID, Reason: strings.TrimSpace(reason)}
}

// book stores new entries with the rate valid on their day as a snapshot.
func (h *handler) book(r *http.Request, cat core.Catalog, entries []core.Entry, reason string) error {
	for _, e := range entries {
		core.ApplyRate(cat, &e)
		if _, err := h.options.Store.CreateEntry(r.Context(), e, change(r, reason)); err != nil {
			return err
		}
	}
	return nil
}

// projectFor resolves the project a free-text line names (@Client/Project or @Project) for the employer, making it
// when it is new.
func (h *handler) projectFor(r *http.Request, cat core.Catalog, emp core.Employer, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	for _, p := range cat.Projects {
		if (p.EmployerID == emp.ID || p.EmployerID == "") && !p.Archived && strings.EqualFold(p.Name, name) {
			return p.ID, nil
		}
	}
	p := core.Project{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, Name: name}
	if err := h.options.Store.Put(r.Context(), p); err != nil {
		return "", err
	}
	return p.ID, nil
}

// projectsOf lists the open projects of an employer.
func projectsOf(cat core.Catalog, employerID string) []core.Project {
	var out []core.Project
	for _, p := range cat.Projects {
		if (p.EmployerID == employerID || p.EmployerID == "") && !p.Archived {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b core.Project) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func projectName(cat core.Catalog, id string) string {
	for _, p := range cat.Projects {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

// storeError turns an error of the core library into a sentence for the person, or "" when it is not theirs to fix.
func (h *handler) storeError(r *http.Request, err error) string {
	var parse *core.ParseError
	switch {
	case errors.As(err, &parse):
		return h.t(r, "track.error.parse", parse.Error())
	case errors.Is(err, core.ErrLocked):
		return h.t(r, "track.error.locked")
	case errors.Is(err, core.ErrConflict):
		return h.t(r, "track.error.conflict")
	case errors.Is(err, core.ErrInvalid):
		return h.t(r, "track.error.invalid", strings.TrimPrefix(err.Error(), "invalid: "))
	case errors.Is(err, core.ErrNotFound):
		return h.t(r, "error.notfound.text")
	case errors.Is(err, errForbidden):
		return h.t(r, "track.error.forbidden")
	case errors.Is(err, auth.ErrTimerRunning):
		return h.t(r, "track.error.timer")
	case errors.Is(err, errLastOrgAdmin):
		return h.t(r, "org.error.lastadmin")
	}
	return ""
}

// back sends the person to the page a form came from, with a notice, or answers the error.
func (h *handler) back(w http.ResponseWriter, r *http.Request, err error, done string) {
	target := safeNext(r.PostFormValue("back"))
	if err != nil {
		if text := h.storeError(r, err); text != "" {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), text)
			return
		}
		h.failed(w, r, err)
		return
	}
	if u, perr := url.Parse(target); perr == nil {
		q := u.Query()
		q.Set("done", done)
		u.RawQuery = q.Encode()
		target = u.String()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
