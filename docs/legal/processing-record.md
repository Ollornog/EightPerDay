# Record of processing activities: time recording and leave (template entry)

> **No legal advice, not checked by a lawyer** (*keine Rechtsberatung, nicht juristisch geprüft*). An entry to adapt
> for the record of processing activities of the employer (Art. 30 Abs. 1 GDPR). The employer is the controller; a
> hoster running EightPerDay for it is a processor (Art. 28 GDPR, contract needed). Square brackets mark what the
> employer fills in.

| Item (Art. 30 Abs. 1 GDPR) | Entry |
|---|---|
| Controller, contact, data protection officer | [name, address], [contact], [DPO, if any: § 38 BDSG, as a rule from 20 persons] |
| Name of the processing | Recording of working time and leave planning with EightPerDay |
| Purposes (lit. b) | Recording of working time as required by law; time and leave accounts; preparation of payroll; planning and approval of leave [; approval of working time] |
| Legal basis | Art. 6 Abs. 1 lit. c GDPR with § 3 Abs. 2 Nr. 1 ArbSchG, § 16 Abs. 2 ArbZG, § 17 MiLoG; Art. 6 Abs. 1 lit. b GDPR (employment contract, § 26 BDSG, whose standing after CJEU C-34/21 is disputed); [works agreement of …, Art. 88 GDPR] |
| Categories of data subjects (lit. c) | Employees [, trainees, temporary staff] |
| Categories of personal data (lit. c) | Working times (start, end, breaks, durations), absences and their type, sickness as a flag without diagnosis, leave and leave notices, corrections with who/when/why, approvals, the access log; field by field in `docs/data-inventory.json` |
| Special categories (Art. 9) | Sickness as a flag only; no notes on health-related absence types |
| Recipients (lit. d) | [HR, payroll, supervisors for data handed in for approval, tax advisor / payroll office, hoster as processor] |
| Transfers to third countries (lit. e) | [None] |
| Time limits for erasure (lit. f) | Time records with history: [3] years after the end of the year (at least 2); leave notices: [3] years; access log: [3] years; automatic purge, legal hold during disputes |
| Technical and organisational measures (lit. g, Art. 32) | Separation of private and company data; tenant separation with row-level security in PostgreSQL; access only through [sign-in gateway]; roles and rights; access log visible to employees, append-only and chained by hash; correction history instead of overwriting; encryption in transit [and at rest]; backups [with deletion]; no tracking, no third-party resources |

## Data protection impact assessment (Art. 35 GDPR)

Time recording alone is not usually high risk. A DPIA is likely needed if the system is used to evaluate the
behaviour or performance of employees with legal effect, or with live presence profiles (German DSK list of
mandatory DPIAs, no. 8). EightPerDay has no performance, location or ranking functions; the supervisor's view shows
only present or absent for the own team, without history. Record here why a DPIA [was / was not] made: [reason].
