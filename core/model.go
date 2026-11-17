// SPDX-License-Identifier: MIT

package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Mode is how a person tracks the time of one employer. The mode only decides which fields are required and how the
// days are calculated; switching it never rewrites stored data (ADR-2).
type Mode string

const (
	// ModeWeekSum keeps one total per week; there is no target and no balance.
	ModeWeekSum Mode = "week_sum"
	// ModeSchedule books the time worked against the target of the schedule: balance and overtime.
	ModeSchedule Mode = "schedule"
	// ModePlan counts the schedule as worked; only deviations are booked, and weeks are confirmed.
	ModePlan Mode = "plan"
	// ModeExact books spans with breaks, split across projects, against the target of the schedule.
	ModeExact Mode = "exact"
	// ModeProjectsOnly books time per project; the sum is no statement about working time.
	ModeProjectsOnly Mode = "projects_only"
)

// Modes lists every mode in the order of how much detail it asks for.
var Modes = []Mode{ModeWeekSum, ModeSchedule, ModePlan, ModeExact, ModeProjectsOnly}

// Valid reports whether m is a known mode.
func (m Mode) Valid() bool {
	for _, x := range Modes {
		if m == x {
			return true
		}
	}
	return false
}

// HasTarget reports whether days in this mode have a target, and so a balance.
func (m Mode) HasTarget() bool { return m == ModeSchedule || m == ModePlan || m == ModeExact }

// EmployerKind tells employers, clients and private work apart. It changes nothing in the calculations.
type EmployerKind string

const (
	KindEmployer EmployerKind = "employer"
	KindClient   EmployerKind = "client"
	KindPrivate  EmployerKind = "private"
)

// Holder says who an employer's data belongs to.
type Holder string

const (
	// HolderPrivate: the person's own records, for example of a second job or a client. Only the person sees them;
	// the person may change, export and erase them freely. This is the default.
	HolderPrivate Holder = "private"
	// HolderManaged: the records belong to an organization (in UnderDoc: the tenant, once it switches company time
	// tracking on). The organization sets the schedule and the rules, the records stay with it when the person
	// leaves, and only the retention purge removes them; corrections go through the history.
	HolderManaged Holder = "managed"
)

// Employer is an employer, a client or private work of one person. Everything else hangs from it: mode, schedule, pay.
type Employer struct {
	ID    string       `json:"id"`
	Owner string       `json:"owner"`
	Name  string       `json:"name"`
	Kind  EmployerKind `json:"kind"`
	// Holder is private (the default when empty) or managed; Organization names the managing organization.
	Holder       Holder `json:"holder,omitempty"`
	Organization string `json:"organization,omitempty"`
	// Mode is the mode from the start; ModeChange records switch it from a date on.
	Mode Mode `json:"mode"`
	// TimeZone is an IANA name; it decides the local date of an entry. Empty means Europe/Berlin.
	TimeZone string `json:"time_zone,omitempty"`
	// Region selects the public holidays, such as "DE-BY" or "AT" (see Regions). Empty means none.
	Region string `json:"region,omitempty"`
	// WeekStart is the first day of the week for weekly totals and confirmations; Monday by default.
	WeekStart time.Weekday `json:"week_start"`
	// DaySplit says which day a span past midnight counts for: the day it starts (the default) or each day its part.
	DaySplit DaySplit `json:"day_split,omitempty"`
	Archived bool     `json:"archived,omitempty"`
	// Hold stops the retention purge for this employer, for example during a dispute (legal hold).
	Hold bool `json:"hold,omitempty"`
}

// Managed reports whether an organization manages the employer.
func (e Employer) Managed() bool { return e.Holder == HolderManaged }

// Location returns the time zone of the employer.
func (e Employer) Location() (*time.Location, error) {
	name := e.TimeZone
	if name == "" {
		name = DefaultTimeZone
	}
	return time.LoadLocation(name)
}

// DefaultTimeZone is used when an employer names none.
const DefaultTimeZone = "Europe/Berlin"

// ModeChange switches the mode of an employer from ValidFrom on.
type ModeChange struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id"`
	ValidFrom  Date   `json:"valid_from"`
	Mode       Mode   `json:"mode"`
}

// OvertimeBasis is the period over which surplus time becomes overtime.
type OvertimeBasis string

const (
	BasisDay   OvertimeBasis = "day"
	BasisWeek  OvertimeBasis = "week"
	BasisMonth OvertimeBasis = "month"
)

// DayPlan is the plan of one weekday. Minutes is the target; Start, End and Break are optional and used when plan
// days become entries (minutes after midnight, Start < End).
type DayPlan struct {
	Minutes int  `json:"minutes"`
	Start   *int `json:"start,omitempty"`
	End     *int `json:"end,omitempty"`
	Break   int  `json:"break,omitempty"`
}

// Schedule is the working time model of an employer from ValidFrom on, until the next schedule starts.
type Schedule struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id"`
	ValidFrom  Date   `json:"valid_from"`
	// Days is indexed by time.Weekday: Days[time.Monday] is Monday.
	Days [7]DayPlan `json:"days"`
	// OvertimeBasis is the period whose surplus counts as overtime; empty means week.
	OvertimeBasis OvertimeBasis `json:"overtime_basis,omitempty"`
	// LeaveDays is the leave entitlement per calendar year while this schedule is valid.
	LeaveDays float64 `json:"leave_days,omitempty"`
}

// WeekMinutes returns the target of a full week.
func (s Schedule) WeekMinutes() int {
	n := 0
	for _, d := range s.Days {
		n += d.Minutes
	}
	return n
}

// PayBasis is how a pay rule pays.
type PayBasis string

const (
	PayHourly     PayBasis = "hourly"
	PayPerProject PayBasis = "per_project"
	// PayPerAssignment pays an amount per working day or per job (see PayRule.Unit), with or without hours.
	PayPerAssignment PayBasis = "per_assignment"
	// PayFlatPeriod pays an amount per week, month or year, prorated by days for parts of it.
	PayFlatPeriod PayBasis = "flat_period"
)

// Units of PayRule.Unit.
const (
	UnitDay        = "day"
	UnitAssignment = "assignment"
	UnitWeek       = "week"
	UnitMonth      = "month"
	UnitYear       = "year"
)

// PayRule says what an employer pays, from ValidFrom to ValidTo (open if zero), for the whole employer or one project.
// Amounts are in minor units (cents). The most specific rule valid on a date wins: project before employer, then the
// later ValidFrom.
type PayRule struct {
	ID         string   `json:"id"`
	Owner      string   `json:"owner"`
	EmployerID string   `json:"employer_id"`
	ProjectID  string   `json:"project_id,omitempty"`
	ValidFrom  Date     `json:"valid_from"`
	ValidTo    Date     `json:"valid_to,omitempty"`
	Basis      PayBasis `json:"basis"`
	Amount     int64    `json:"amount"`
	Currency   string   `json:"currency"`
	// Unit: for per_assignment "day" (default) or "assignment"; for flat_period "week", "month" (default) or "year".
	Unit string `json:"unit,omitempty"`
}

// Client is a customer below an employer or client.
type Client struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id,omitempty"`
	Name       string `json:"name"`
	Archived   bool   `json:"archived,omitempty"`
}

// Project groups entries. CompletedOn triggers a per_project pay rule.
type Project struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	EmployerID  string `json:"employer_id,omitempty"`
	ClientID    string `json:"client_id,omitempty"`
	Name        string `json:"name"`
	Billable    bool   `json:"billable,omitempty"`
	CompletedOn Date   `json:"completed_on,omitempty"`
	// TargetMinutes per TargetPeriod (week, month) is an optional goal, mostly for ModeProjectsOnly.
	TargetMinutes int    `json:"target_minutes,omitempty"`
	TargetPeriod  string `json:"target_period,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
}

// Activity is a kind of work, optionally tied to a project.
type Activity struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	ProjectID string `json:"project_id,omitempty"`
	Name      string `json:"name"`
	Archived  bool   `json:"archived,omitempty"`
}

// Tag is a label with a path such as "customer/meeting".
type Tag struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Path  string `json:"path"`
	Color string `json:"color,omitempty"`
}

// EntryKind is what an entry books.
type EntryKind string

const (
	EntryWork  EntryKind = "work"
	EntryBreak EntryKind = "break"
	// EntryAdjust is a signed deviation from the plan, such as +90 (longer) or -120 (left early).
	EntryAdjust EntryKind = "adjust"
)

// Precision says how exactly the time of an entry is known.
type Precision string

const (
	PrecisionSpan Precision = "span"
	PrecisionDay  Precision = "day"
	PrecisionWeek Precision = "week"
)

// Source says where an entry came from.
type Source string

const (
	SourceManual     Source = "manual"
	SourceTimer      Source = "timer"
	SourceParse      Source = "parse"
	SourcePlan       Source = "plan"
	SourceSuggestion Source = "suggestion"
	SourceImport     Source = "import"
)

// RateSnapshot is the pay rate applied to an entry when it was booked. It changes only through Revalue.
type RateSnapshot struct {
	RuleID   string `json:"rule_id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Entry is one booking. Minutes is always set and is the truth for sums; Start and End are optional detail and, if
// both are set, Minutes is derived from them (Normalize).
type Entry struct {
	ID         string    `json:"id"`
	Owner      string    `json:"owner"`
	EmployerID string    `json:"employer_id"`
	Kind       EntryKind `json:"kind"`
	// Date is the local date the entry belongs to; for a week entry the first day of the week.
	Date      Date       `json:"date"`
	TimeZone  string     `json:"time_zone,omitempty"`
	Start     *time.Time `json:"start,omitempty"`
	End       *time.Time `json:"end,omitempty"`
	Minutes   int        `json:"minutes"`
	Precision Precision  `json:"precision"`
	// Assignment groups entries of one job for per_assignment pay with unit "assignment".
	Assignment string        `json:"assignment,omitempty"`
	ProjectID  string        `json:"project_id,omitempty"`
	ActivityID string        `json:"activity_id,omitempty"`
	Tags       []string      `json:"tags,omitempty"`
	Note       string        `json:"note,omitempty"`
	Source     Source        `json:"source"`
	Billable   bool          `json:"billable,omitempty"`
	Rate       *RateSnapshot `json:"rate,omitempty"`
	// ApprovedAt and ApprovedBy are the approval hook of the company features.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	ApprovedBy string     `json:"approved_by,omitempty"`
	Version    int        `json:"version"`
	Created    time.Time  `json:"created"`
	Changed    time.Time  `json:"changed"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

// AbsenceEffect is what an absence does to the target of a day.
type AbsenceEffect string

const (
	// FillsTarget: the absent time counts as worked (credited), the target stays.
	FillsTarget AbsenceEffect = "fills_target"
	// ReducesTarget: the target of the absent time falls away.
	ReducesTarget AbsenceEffect = "reduces_target"
	// InfoOnly: nothing changes; the time is missing from the balance (for example time off in lieu).
	InfoOnly AbsenceEffect = "info"
)

// AbsenceType describes a kind of absence. The key identifies it; an owner can override the built-in types.
type AbsenceType struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	// EmployerID binds the type to one employer; empty means every private employer of the owner. A managed employer
	// only sees the built-in types and its own.
	EmployerID string        `json:"employer_id,omitempty"`
	Key        string        `json:"key"`
	Name       string        `json:"name"`
	Effect     AbsenceEffect `json:"effect"`
	// DeductsLeave: the days count against the leave entitlement.
	DeductsLeave bool `json:"deducts_leave,omitempty"`
	// Health marks types that reveal health data: no note is stored for them.
	Health        bool `json:"health,omitempty"`
	NeedsApproval bool `json:"needs_approval,omitempty"`
}

// Built-in absence type keys.
const (
	AbsenceVacation = "vacation"
	AbsenceSick     = "sick"
	AbsenceHoliday  = "holiday"
	AbsenceComp     = "comp"
	AbsenceUnpaid   = "unpaid"
	AbsenceOther    = "other"
)

// DefaultAbsenceTypes returns the built-in types. They reduce the target, as most payroll rules do (adjusted target =
// target - holidays - leave - sickness); time off in lieu and other absences are for information only.
func DefaultAbsenceTypes() []AbsenceType {
	return []AbsenceType{
		{Key: AbsenceVacation, Name: "Vacation", Effect: ReducesTarget, DeductsLeave: true},
		{Key: AbsenceSick, Name: "Sick", Effect: ReducesTarget, Health: true},
		{Key: AbsenceHoliday, Name: "Day off", Effect: ReducesTarget},
		{Key: AbsenceComp, Name: "Time off in lieu", Effect: InfoOnly},
		{Key: AbsenceUnpaid, Name: "Unpaid leave", Effect: ReducesTarget},
		{Key: AbsenceOther, Name: "Other", Effect: InfoOnly},
	}
}

// AbsenceStatus is the approval state. Without the company feature an absence is approved when it is created.
type AbsenceStatus string

const (
	StatusRequested AbsenceStatus = "requested"
	StatusApproved  AbsenceStatus = "approved"
	StatusRejected  AbsenceStatus = "rejected"
)

// Absence covers the days From to To, both included. EmployerID empty means every private employer of the owner
// (sickness usually is); a managed employer only sees absences booked for it, so nothing private reaches an
// organization without the person's own step. HalfDay makes every covered day count half.
type Absence struct {
	ID         string        `json:"id"`
	Owner      string        `json:"owner"`
	EmployerID string        `json:"employer_id,omitempty"`
	Type       string        `json:"type"`
	From       Date          `json:"from"`
	To         Date          `json:"to"`
	HalfDay    bool          `json:"half_day,omitempty"`
	Status     AbsenceStatus `json:"status"`
	ApprovedBy string        `json:"approved_by,omitempty"`
	Note       string        `json:"note,omitempty"`
	Version    int           `json:"version"`
	Created    time.Time     `json:"created"`
	Changed    time.Time     `json:"changed"`
	DeletedAt  *time.Time    `json:"deleted_at,omitempty"`
}

// Confirmation records that the person declared the plan days From to To as worked (ModePlan, "the week is right").
type Confirmation struct {
	ID          string    `json:"id"`
	Owner       string    `json:"owner"`
	EmployerID  string    `json:"employer_id"`
	From        Date      `json:"from"`
	To          Date      `json:"to"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// AdjustmentKind is what a balance adjustment changes.
type AdjustmentKind string

const (
	AdjustOvertime AdjustmentKind = "overtime"
	AdjustLeave    AdjustmentKind = "leave"
)

// Adjustment changes an account on a date: a carry-over, a payout, a correction. Minutes for overtime, Days for leave.
type Adjustment struct {
	ID         string         `json:"id"`
	Owner      string         `json:"owner"`
	EmployerID string         `json:"employer_id"`
	Date       Date           `json:"date"`
	Kind       AdjustmentKind `json:"kind"`
	Minutes    int            `json:"minutes,omitempty"`
	Days       float64        `json:"days,omitempty"`
	Reason     string         `json:"reason"`
}

// PeriodClose closes the days From to To of an employer: entries and absences there can no longer change. The values
// at closing are kept.
type PeriodClose struct {
	ID         string    `json:"id"`
	Owner      string    `json:"owner"`
	EmployerID string    `json:"employer_id"`
	From       Date      `json:"from"`
	To         Date      `json:"to"`
	Target     int       `json:"target"`
	Actual     int       `json:"actual"`
	Balance    int       `json:"balance"`
	ClosedAt   time.Time `json:"closed_at"`
	ClosedBy   string    `json:"closed_by,omitempty"`
}

// Holiday is a day off of one's own, next to the public holidays of the region: for one employer, or for every
// private employer of the owner if EmployerID is empty. Fraction is in thousandths (1000 a full day, 500 half).
type Holiday struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id,omitempty"`
	Date       Date   `json:"date"`
	Name       string `json:"name"`
	Fraction   int    `json:"fraction"`
}

// LeaveNotice records that the employer asked the person to take the leave of Year and pointed out that it lapses
// otherwise. Lapse and limitation depend on this notice, so it is kept with its time.
type LeaveNotice struct {
	ID         string    `json:"id"`
	Owner      string    `json:"owner"`
	EmployerID string    `json:"employer_id"`
	Year       int       `json:"year"`
	NotifiedAt time.Time `json:"notified_at"`
	Text       string    `json:"text"`
}

// PurgeLog records a run of the retention purge. Owner is empty when the run covered every owner.
type PurgeLog struct {
	ID      string    `json:"id"`
	Owner   string    `json:"owner"`
	Class   Class     `json:"class"`
	Cutoff  Date      `json:"cutoff"`
	Removed int       `json:"removed"`
	At      time.Time `json:"at"`
}

// NewID returns a random identifier: 32 hexadecimal characters.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b[:])
}

// ErrInvalid wraps every validation error.
var ErrInvalid = errors.New("invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

var (
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	tagPattern      = regexp.MustCompile(`^[\p{L}\p{N}_.-]+(/[\p{L}\p{N}_.-]+)*$`)
)

func checkKeys(id, owner string) error {
	if !idPattern.MatchString(id) {
		return invalid("id %q", id)
	}
	if !idPattern.MatchString(owner) {
		return invalid("owner %q", owner)
	}
	return nil
}

func checkRef(name, v string, required bool) error {
	if v == "" && !required {
		return nil
	}
	if !idPattern.MatchString(v) {
		return invalid("%s %q", name, v)
	}
	return nil
}

func checkName(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 200 {
		return invalid("name must have 1 to 200 characters")
	}
	return nil
}

// Validate checks the employer.
func (e Employer) Validate() error {
	if err := checkKeys(e.ID, e.Owner); err != nil {
		return err
	}
	if err := checkName(e.Name); err != nil {
		return err
	}
	switch e.Kind {
	case KindEmployer, KindClient, KindPrivate:
	default:
		return invalid("employer kind %q", e.Kind)
	}
	if !e.Mode.Valid() {
		return invalid("mode %q", e.Mode)
	}
	switch e.Holder {
	case "", HolderPrivate:
		if e.Organization != "" {
			return invalid("a private employer has no organization")
		}
	case HolderManaged:
		if err := checkRef("organization", e.Organization, true); err != nil {
			return err
		}
	default:
		return invalid("holder %q", e.Holder)
	}
	if _, err := e.Location(); err != nil {
		return invalid("time zone %q", e.TimeZone)
	}
	if e.Region != "" && !KnownRegion(e.Region) {
		return invalid("region %q", e.Region)
	}
	if e.WeekStart < time.Sunday || e.WeekStart > time.Saturday {
		return invalid("week start %d", e.WeekStart)
	}
	if !e.DaySplit.Valid() {
		return invalid("day split %q", e.DaySplit)
	}
	return nil
}

// Validate checks the mode change.
func (m ModeChange) Validate() error {
	if err := checkKeys(m.ID, m.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", m.EmployerID, true); err != nil {
		return err
	}
	if m.ValidFrom.IsZero() {
		return invalid("mode change without valid-from date")
	}
	if !m.Mode.Valid() {
		return invalid("mode %q", m.Mode)
	}
	return nil
}

// Validate checks the schedule.
func (s Schedule) Validate() error {
	if err := checkKeys(s.ID, s.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", s.EmployerID, true); err != nil {
		return err
	}
	if s.ValidFrom.IsZero() {
		return invalid("schedule without valid-from date")
	}
	for i, d := range s.Days {
		if d.Minutes < 0 || d.Minutes > 24*60 {
			return invalid("%s: target %d minutes", time.Weekday(i), d.Minutes)
		}
		if d.Break < 0 || d.Break > d.Minutes+24*60 {
			return invalid("%s: break %d minutes", time.Weekday(i), d.Break)
		}
		if (d.Start == nil) != (d.End == nil) {
			return invalid("%s: start and end go together", time.Weekday(i))
		}
		if d.Start != nil && (*d.Start < 0 || *d.End > 48*60 || *d.Start >= *d.End) {
			return invalid("%s: start must be before end", time.Weekday(i))
		}
	}
	switch s.OvertimeBasis {
	case "", BasisDay, BasisWeek, BasisMonth:
	default:
		return invalid("overtime basis %q", s.OvertimeBasis)
	}
	if s.LeaveDays < 0 || s.LeaveDays > 366 {
		return invalid("leave days %v", s.LeaveDays)
	}
	return nil
}

// Validate checks the pay rule.
func (p PayRule) Validate() error {
	if err := checkKeys(p.ID, p.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", p.EmployerID, true); err != nil {
		return err
	}
	if err := checkRef("project", p.ProjectID, false); err != nil {
		return err
	}
	if p.ValidFrom.IsZero() {
		return invalid("pay rule without valid-from date")
	}
	if !p.ValidTo.IsZero() && p.ValidTo.Before(p.ValidFrom) {
		return invalid("pay rule ends before it starts")
	}
	if p.Amount < 0 {
		return invalid("negative amount")
	}
	if !currencyPattern.MatchString(p.Currency) {
		return invalid("currency %q", p.Currency)
	}
	switch p.Basis {
	case PayHourly:
		if p.Unit != "" {
			return invalid("hourly rule with unit %q", p.Unit)
		}
	case PayPerProject:
		if p.ProjectID == "" {
			return invalid("per_project rule without project")
		}
	case PayPerAssignment:
		if p.Unit != "" && p.Unit != UnitDay && p.Unit != UnitAssignment {
			return invalid("per_assignment unit %q", p.Unit)
		}
	case PayFlatPeriod:
		if p.Unit != "" && p.Unit != UnitWeek && p.Unit != UnitMonth && p.Unit != UnitYear {
			return invalid("flat_period unit %q", p.Unit)
		}
	default:
		return invalid("pay basis %q", p.Basis)
	}
	return nil
}

// Validate checks the client.
func (c Client) Validate() error {
	if err := checkKeys(c.ID, c.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", c.EmployerID, false); err != nil {
		return err
	}
	return checkName(c.Name)
}

// Validate checks the project.
func (p Project) Validate() error {
	if err := checkKeys(p.ID, p.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", p.EmployerID, false); err != nil {
		return err
	}
	if err := checkRef("client", p.ClientID, false); err != nil {
		return err
	}
	if p.TargetMinutes < 0 {
		return invalid("negative project target")
	}
	switch p.TargetPeriod {
	case "", UnitDay, UnitWeek, UnitMonth, UnitYear:
	default:
		return invalid("target period %q", p.TargetPeriod)
	}
	return checkName(p.Name)
}

// Validate checks the activity.
func (a Activity) Validate() error {
	if err := checkKeys(a.ID, a.Owner); err != nil {
		return err
	}
	if err := checkRef("project", a.ProjectID, false); err != nil {
		return err
	}
	return checkName(a.Name)
}

// Validate checks the tag.
func (t Tag) Validate() error {
	if err := checkKeys(t.ID, t.Owner); err != nil {
		return err
	}
	if !tagPattern.MatchString(t.Path) || len(t.Path) > 200 {
		return invalid("tag %q", t.Path)
	}
	return nil
}

// Validate checks the entry. Normalize first, so Minutes matches Start and End.
func (e Entry) Validate() error {
	if err := checkKeys(e.ID, e.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", e.EmployerID, true); err != nil {
		return err
	}
	for name, v := range map[string]string{"project": e.ProjectID, "activity": e.ActivityID} {
		if err := checkRef(name, v, false); err != nil {
			return err
		}
	}
	if e.Date.IsZero() {
		return invalid("entry without date")
	}
	switch e.Kind {
	case EntryWork, EntryBreak:
		if e.Minutes < 0 {
			return invalid("negative minutes")
		}
	case EntryAdjust:
		if e.Start != nil || e.End != nil {
			return invalid("an adjustment has no start or end")
		}
	default:
		return invalid("entry kind %q", e.Kind)
	}
	if e.Minutes > 7*24*60 || e.Minutes < -24*60 {
		return invalid("minutes %d out of range", e.Minutes)
	}
	switch e.Precision {
	case PrecisionSpan:
		if e.Start == nil || e.End == nil {
			return invalid("a span needs start and end")
		}
		if !e.End.After(*e.Start) {
			return invalid("end must be after start")
		}
		if e.End.Sub(*e.Start) > 24*time.Hour {
			return invalid("a span is at most 24 hours")
		}
	case PrecisionDay, PrecisionWeek:
		if (e.Start == nil) != (e.End == nil) {
			return invalid("start and end go together")
		}
	default:
		return invalid("precision %q", e.Precision)
	}
	switch e.Source {
	case SourceManual, SourceTimer, SourceParse, SourcePlan, SourceSuggestion, SourceImport:
	default:
		return invalid("source %q", e.Source)
	}
	for _, t := range e.Tags {
		if !tagPattern.MatchString(t) {
			return invalid("tag %q", t)
		}
	}
	if len(e.Note) > 2000 {
		return invalid("note longer than 2000 characters")
	}
	if e.Rate != nil && !currencyPattern.MatchString(e.Rate.Currency) {
		return invalid("rate currency %q", e.Rate.Currency)
	}
	return nil
}

// Validate checks the absence type.
func (t AbsenceType) Validate() error {
	if err := checkKeys(t.ID, t.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", t.EmployerID, false); err != nil {
		return err
	}
	if !idPattern.MatchString(t.Key) {
		return invalid("absence type key %q", t.Key)
	}
	switch t.Effect {
	case FillsTarget, ReducesTarget, InfoOnly:
	default:
		return invalid("absence effect %q", t.Effect)
	}
	return checkName(t.Name)
}

// Validate checks the absence. Types that reveal health data take no note (data minimisation).
func (a Absence) Validate(types []AbsenceType) error {
	if err := checkKeys(a.ID, a.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", a.EmployerID, false); err != nil {
		return err
	}
	t, ok := findAbsenceType(types, a.Type)
	if !ok {
		return invalid("absence type %q", a.Type)
	}
	if a.From.IsZero() || a.To.IsZero() || a.To.Before(a.From) {
		return invalid("absence needs from <= to")
	}
	if a.From.DaysUntil(a.To) > 366 {
		return invalid("absence longer than a year")
	}
	switch a.Status {
	case StatusRequested, StatusApproved, StatusRejected:
	default:
		return invalid("absence status %q", a.Status)
	}
	if t.Health && a.Note != "" {
		return invalid("no note for %q: it would hold health data", a.Type)
	}
	if len(a.Note) > 2000 {
		return invalid("note longer than 2000 characters")
	}
	return nil
}

// Validate checks the confirmation.
func (c Confirmation) Validate() error {
	if err := checkKeys(c.ID, c.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", c.EmployerID, true); err != nil {
		return err
	}
	if c.From.IsZero() || c.To.Before(c.From) {
		return invalid("confirmation needs from <= to")
	}
	return nil
}

// Validate checks the adjustment.
func (a Adjustment) Validate() error {
	if err := checkKeys(a.ID, a.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", a.EmployerID, true); err != nil {
		return err
	}
	if a.Date.IsZero() {
		return invalid("adjustment without date")
	}
	switch a.Kind {
	case AdjustOvertime, AdjustLeave:
	default:
		return invalid("adjustment kind %q", a.Kind)
	}
	if strings.TrimSpace(a.Reason) == "" {
		return invalid("an adjustment needs a reason")
	}
	return nil
}

// Validate checks the period close.
func (p PeriodClose) Validate() error {
	if err := checkKeys(p.ID, p.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", p.EmployerID, true); err != nil {
		return err
	}
	if p.From.IsZero() || p.To.Before(p.From) {
		return invalid("period close needs from <= to")
	}
	return nil
}

// Validate checks the holiday.
func (h Holiday) Validate() error {
	if err := checkKeys(h.ID, h.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", h.EmployerID, false); err != nil {
		return err
	}
	if h.Date.IsZero() {
		return invalid("holiday without date")
	}
	if h.Fraction <= 0 || h.Fraction > 1000 {
		return invalid("holiday fraction %d (1 to 1000)", h.Fraction)
	}
	return checkName(h.Name)
}

// Validate checks the leave notice.
func (n LeaveNotice) Validate() error {
	if err := checkKeys(n.ID, n.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", n.EmployerID, true); err != nil {
		return err
	}
	if n.Year < 1900 || n.Year > 9999 || n.NotifiedAt.IsZero() {
		return invalid("leave notice needs a year and a time")
	}
	return nil
}

// Validate checks the purge log.
func (p PurgeLog) Validate() error {
	if err := checkRef("id", p.ID, true); err != nil {
		return err
	}
	return checkRef("owner", p.Owner, false)
}

func findAbsenceType(types []AbsenceType, key string) (AbsenceType, bool) {
	for _, t := range types {
		if t.Key == key {
			return t, true
		}
	}
	return AbsenceType{}, false
}
