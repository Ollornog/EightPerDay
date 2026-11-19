// SPDX-License-Identifier: MIT

package core

import (
	"reflect"
	"strings"
)

// The inventory of the data the library stores (A21 of the legal notes): for every record, what it is for, who it
// belongs to, how it is deleted, and every field with its purpose. It is built from the types themselves, and a test
// keeps docs/data-inventory.json in step with it and fails when a field has no description. It helps with the record
// of processing activities and the information of the people concerned; it is no legal assessment.

// DataField is one field of a record.
type DataField struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

// DataKind is one kind of record.
type DataKind struct {
	// Name is the name of the Go type; Kind the catalog kind, where it is one.
	Name    string `json:"name"`
	Kind    Kind   `json:"kind,omitempty"`
	Purpose string `json:"purpose"`
	// Holder says whose data it is and who sees it.
	Holder string `json:"holder"`
	// Class is the deletion class of the retention purge, if any; Deletion says how the record goes otherwise.
	Class    Class       `json:"deletion_class,omitempty"`
	Deletion string      `json:"deletion"`
	Fields   []DataField `json:"fields"`
}

// Data holders and deletions that many records share.
const (
	holderOfEmployer = "The person; for a managed employer also the organization (Store.Managed). Records for every " +
		"employer of a person count only for private employers."
	holderOfPerson      = "The person alone."
	holderOfOrg         = "The organization and its administrators."
	deletedWithEmployer = "Deleted by whoever may change the employer's settings; erased with the private part of a " +
		"person (Store.Erase); records of a managed employer stay with the organization."
	deletedByPurge = "Removed by the retention purge of its class (Store.Purge), except for employers on hold; the " +
		"private part also by Store.Erase."
)

// commonFields describe the fields that mean the same in every record.
var commonFields = map[string]string{
	"id":          "Identifier of the record.",
	"owner":       "The person the record belongs to.",
	"employer_id": "The employer, client or private work the record is for; empty means every private employer.",
	"name":        "Name shown to the person.",
	"archived":    "Hidden from new bookings, kept for the past.",
	"valid_from":  "First day the record applies.",
	"valid_to":    "Last day the record applies; empty means open-ended.",
	"from":        "First day covered.",
	"to":          "Last day covered.",
	"version":     "Version for the correction history; every change counts it up.",
	"created":     "When the record was created.",
	"changed":     "When the record was last changed.",
	"deleted_at":  "When the record was marked as deleted; the history keeps what it was.",
}

// inventory lists the records with their purpose and the fields that need words of their own.
var inventory = []struct {
	value    any
	kind     Kind
	purpose  string
	holder   string
	class    Class
	deletion string
	fields   map[string]string
}{
	{Employer{}, RecEmployer, "An employer, client or private work of a person, with how its time is tracked.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{
			"kind":         "Employer, client or private work.",
			"holder":       "Private (the person's own records) or managed by an organization.",
			"organization": "The managing organization.",
			"mode":         "How the time is tracked from the start.",
			"time_zone":    "Time zone that decides the local date of a booking.",
			"region":       "Region whose public holidays apply.",
			"week_start":   "First day of the week for weekly totals.",
			"day_split":    "Which day time past midnight counts for.",
			"hold":         "Legal hold: stops the retention purge, for example during a dispute.",
		}},
	{ModeChange{}, RecModeChange, "Switches the tracking mode of an employer from a date on.", holderOfEmployer, "",
		deletedWithEmployer, map[string]string{"mode": "The mode from valid_from on."}},
	{Schedule{}, RecSchedule, "Working time model: target per weekday, overtime basis, leave entitlement.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{
			"days":           "Target minutes per weekday, with optional start, end and break.",
			"overtime_basis": "Period whose surplus counts as overtime (day, week, month).",
			"leave_days":     "Leave entitlement per calendar year.",
		}},
	{PayRule{}, RecPayRule, "What an employer pays for work.", holderOfEmployer, "", deletedWithEmployer,
		map[string]string{
			"project_id": "Project the rule is limited to.",
			"basis":      "Per hour, per project, per assignment or per period.",
			"amount":     "Amount in minor units.",
			"currency":   "Currency (ISO 4217).",
			"unit":       "Day, assignment, week, month or year, depending on the basis.",
			"salaries":   "Monthly salaries a year of a monthly rule (13th and 14th month pay).",
		}},
	{Income{}, RecIncome, "Money earned on a day beyond the pay rules: a job with its client, commission, photos, tips.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{
			"date":     "Day it was earned.",
			"kind":     "Job, commission, photos, tip or other.",
			"client":   "Customer of a job, as typed.",
			"amount":   "Amount in minor units.",
			"currency": "Currency (ISO 4217).",
			"paid_by":  "Taken in cash, by card or by transfer.",
			"note":     "The person's note.",
		}},
	{DayInfo{}, RecDayInfo, "Details of a day of an employer: place of work, kilometres, standby.", holderOfEmployer, "",
		deletedWithEmployer, map[string]string{
			"date":            "The day.",
			"place":           "Where the work was, as typed.",
			"km_one_way":      "Kilometres of the way there, in tenths.",
			"km_total":        "Kilometres of the whole day, in tenths.",
			"standby_passive": "Minutes on standby, reachable only.",
			"standby_active":  "Minutes on standby, called out.",
		}},
	{Client{}, RecClient, "A customer below an employer, for grouping projects.", holderOfEmployer, "",
		deletedWithEmployer, nil},
	{Project{}, RecProject, "A project that bookings belong to.", holderOfEmployer, "", deletedWithEmployer,
		map[string]string{
			"client_id":      "The client of the project.",
			"number":         "Order or job number the work is reported under.",
			"billable":       "Whether its time is billed.",
			"completed_on":   "Day of completion; triggers a pay rule per project.",
			"target_minutes": "Optional goal of minutes per target_period.",
			"target_period":  "Period of the goal.",
		}},
	{Activity{}, RecActivity, "A kind of work, optionally tied to a project.", holderOfPerson, "",
		deletedWithEmployer, map[string]string{"project_id": "Project the activity belongs to."}},
	{Tag{}, RecTag, "A label for bookings.", holderOfPerson, "", deletedWithEmployer, map[string]string{
		"path":  "The label, with / for levels.",
		"color": "Display colour.",
	}},
	{AbsenceType{}, RecAbsenceType, "An own kind of absence and its effect on the target.", holderOfEmployer, "",
		deletedWithEmployer, map[string]string{
			"key":            "Identifies the type; overrides a built-in one of the same key.",
			"effect":         "Fills the target, reduces it, or information only.",
			"deducts_leave":  "Counts against the leave entitlement.",
			"health":         "Reveals health data: no note is stored for it.",
			"needs_approval": "Absences of the type wait for approval.",
		}},
	{Confirmation{}, RecConfirmation, "The person's statement that the planned days were worked as planned.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{"confirmed_at": "When the person confirmed."}},
	{Adjustment{}, RecAdjustment, "A change of the time or leave account: carry-over, payout, correction.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{
			"date":    "Day the change counts on.",
			"kind":    "Time account (overtime) or leave account.",
			"minutes": "Minutes for the time account.",
			"days":    "Days for the leave account.",
			"reason":  "Why the account changed.",
		}},
	{PeriodClose{}, RecPeriodClose, "Closes a period: its records can no longer change.", holderOfEmployer, "",
		"Never deleted on its own; erased with the private part (Store.Erase).", map[string]string{
			"target":    "Target minutes when closing.",
			"actual":    "Actual minutes when closing.",
			"balance":   "Balance when closing.",
			"closed_at": "When the period was closed.",
			"closed_by": "Who closed it.",
		}},
	{Holiday{}, RecHoliday, "A day off of one's own next to the public holidays.", holderOfEmployer, "",
		deletedWithEmployer, map[string]string{
			"date":     "The day.",
			"fraction": "Share of the day off, in thousandths.",
		}},
	{LeaveNotice{}, RecLeaveNotice, "Proof that the employer asked the person to take the leave of a year and " +
		"pointed out that it lapses otherwise.", holderOfEmployer, ClassLeaveNotices, deletedByPurge,
		map[string]string{
			"year":        "Leave year the notice is about.",
			"notified_at": "When the person was notified.",
			"text":        "The text of the notice.",
		}},
	{PurgeLog{}, RecPurgeLog, "Proof of a run of the retention purge.", "Whoever runs the purge.", "",
		"Never deleted by the library; erased with the person's records (Store.Erase).", map[string]string{
			"owner":   "The person whose records were purged; empty for a run over everyone.",
			"class":   "Deletion class of the run.",
			"cutoff":  "Records dated before this day were removed.",
			"removed": "How many records went.",
			"at":      "When the purge ran.",
		}},
	{AbsencePay{}, RecAbsencePay, "Says that an employer pays an absence type, and how.", holderOfEmployer, "",
		deletedWithEmployer, map[string]string{
			"type":   "Absence type key, or public_holiday.",
			"method": "At the target minutes or at the average of the reference weeks.",
			"weeks":  "Reference weeks of the average.",
		}},
	{Surcharge{}, RecSurcharge, "A percentage on top of the pay for night, Sunday, holiday or overtime minutes.",
		holderOfEmployer, "", deletedWithEmployer, map[string]string{
			"kind":         "Night, Sunday, holiday or overtime.",
			"basis_points": "The percentage in hundredths of a percent.",
			"base_amount":  "Hourly base in minor units, for staff without an hourly rule.",
			"currency":     "Currency of the base.",
			"window_start": "Start of the night window, minutes after midnight.",
			"window_end":   "End of the night window, minutes after midnight.",
		}},
	{Submission{}, RecSubmission, "A period or leave request handed in for approval, or a correction of one " +
		"(company feature).", holderOfEmployer, "", "Never deleted on its own; stays with the organization " +
		"like the records of its managed employer.", map[string]string{
		"kind":         "Period, leave request or correction.",
		"absence_id":   "The absence a leave request asks for.",
		"supersedes":   "The approved submission a correction asks to open again.",
		"status":       "Submitted, approved, rejected or withdrawn.",
		"note":         "The person's note; for a correction why.",
		"submitted_at": "When it was handed in.",
		"target":       "Target minutes of the period when handed in.",
		"actual":       "Actual minutes of the period when handed in.",
		"balance":      "Balance of the period when handed in.",
		"decided_at":   "When it was decided or withdrawn.",
		"decided_by":   "Who decided or withdrew it.",
		"reason":       "Reason of the decision; always given for a rejection.",
		"proposal":     "The values a change request asks the days of an approved period to hold: hours or an absence, and a note.",
	}},
	{Entry{}, "", "A booking of working time, a break or a deviation from the plan.", holderOfEmployer,
		ClassTimeRecords, deletedByPurge, map[string]string{
			"kind":        "Work, break or deviation.",
			"date":        "Local date the booking counts for.",
			"time_zone":   "Time zone it was booked in.",
			"start":       "Start, in UTC.",
			"end":         "End, in UTC.",
			"minutes":     "Duration in minutes; the truth for every sum.",
			"precision":   "Span, day or week.",
			"assignment":  "Groups bookings of one job for pay per assignment.",
			"project_id":  "Project of the booking.",
			"activity_id": "Activity of the booking.",
			"tags":        "Labels.",
			"note":        "The person's note.",
			"source":      "Where it came from: by hand, timer, free text, plan, suggestion, import.",
			"billable":    "Whether the time is billed.",
			"rate":        "Hourly rate applied when booked.",
			"approved_at": "When it was approved (company feature).",
			"approved_by": "Who approved it.",
		}},
	{Absence{}, "", "An absence over days: leave, sickness (a flag without details), days off.", holderOfEmployer,
		ClassTimeRecords, deletedByPurge, map[string]string{
			"type":        "Absence type key.",
			"half_day":    "Every covered day counts half.",
			"status":      "Requested, approved or rejected.",
			"approved_by": "Who decided on it.",
			"note":        "The person's note; never for types with health data.",
		}},
	{Revision{}, "", "One step of the correction history of an entry or absence: who changed what, when and why.",
		holderOfEmployer, ClassTimeRecords, "Goes with its record.", map[string]string{
			"type":      "Entry or absence.",
			"record_id": "The record changed.",
			"version":   "Version the change produced.",
			"op":        "Created, changed or deleted.",
			"at":        "When.",
			"by":        "Who.",
			"reason":    "Why.",
			"before":    "The record before the change.",
			"after":     "The record after the change.",
		}},
	{Organization{}, "", "A body that manages the time tracking of its members.", holderOfOrg, "",
		"Kept until its administrators remove it; no erasure of a person reaches it.", map[string]string{
			"time_zone": "Default time zone for new members.",
			"region":    "Default holiday region for new members.",
			"features":  "Company features (approval, supervisor view), off by default, with the works agreement and who confirmed it when.",
			"retention": "Retention per deletion class.",
			"templates": "Schedules offered to members.",
		}},
	{Membership{}, "", "Ties a person to an organization for a period, with a role.", holderOfOrg, "",
		"Ended, never deleted; stays with the organization.", map[string]string{
			"organization": "The organization.",
			"person":       "The member.",
			"role":         "Member or administrator.",
			"employer_id":  "The managed employer that holds the member's records.",
			"from":         "First day of the membership.",
			"until":        "Last day; empty while it lasts.",
		}},
	{Timer{}, "", "A running stopwatch: at most one per person, until it is stopped and booked as an entry.",
		holderOfPerson, "", "Removed when it is stopped, and with the private part of a person (Store.Erase).",
		map[string]string{
			"project_id": "The project the time will be booked on.",
			"note":       "The person's note for the entry.",
			"started":    "When the clock started.",
		}},
	{Access{}, "", "The access log: who read, changed, exported or decided whose data when.",
		"The person concerned (their own log) and whoever runs the instance.", ClassAccessLog,
		"Only added; removed by the retention purge, never before two years.", map[string]string{
			"at":           "When.",
			"actor":        "Who.",
			"organization": "On behalf of which organization.",
			"subject":      "Whose data.",
			"action":       "Read, change, export, decision or role change.",
			"what":         "Which data, without the data itself.",
			"from":         "First day of the data touched.",
			"to":           "Last day of the data touched.",
			"prev":         "Hash of the subject's entry before.",
			"hash":         "Hash of this entry.",
		}},
}

// jsonFields returns the JSON names of the fields of a struct value.
func jsonFields(v any) []string {
	rt := reflect.TypeOf(v)
	var out []string
	for i := range rt.NumField() {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// DataInventory returns the inventory of every record the library stores, field by field. A field without a
// description of its own or a shared one gets an empty purpose; the test of the library refuses that.
func DataInventory() []DataKind {
	out := make([]DataKind, 0, len(inventory))
	for _, x := range inventory {
		k := DataKind{Name: reflect.TypeOf(x.value).Name(), Kind: x.kind, Purpose: x.purpose, Holder: x.holder,
			Class: x.class, Deletion: x.deletion}
		for _, f := range jsonFields(x.value) {
			p, ok := x.fields[f]
			if !ok {
				p = commonFields[f]
			}
			k.Fields = append(k.Fields, DataField{Name: f, Purpose: p})
		}
		out = append(out, k)
	}
	return out
}
