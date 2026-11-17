# Templates for employers

> **No legal advice, not checked by a lawyer** (*keine Rechtsberatung, nicht juristisch geprüft*). These are drafts
> to start from, written from the software's side. Whoever uses EightPerDay as an employer is responsible for its
> lawful use and has the texts checked and adapted, together with the works council and the data protection officer
> where there are any. The legal situation can change; the notes name the state they were written in (November 2026).

| File | What |
|---|---|
| [works-agreement.md](works-agreement.md) | Skeleton of a works agreement (*Betriebsvereinbarung*) on time recording and leave with EightPerDay |
| [processing-record.md](processing-record.md) | Entry for the record of processing activities (Art. 30 GDPR, *Verzeichnis von Verarbeitungstätigkeiten*) |
| [../data-inventory.json](../data-inventory.json) | Every field the library stores, its purpose and its deletion, generated from the code |

## What the software does to help

- Company features (approval, a supervisor's present/absent view) are off by default and can only be switched on with
  the reference to a works agreement and who confirmed it when (`CompanyFeatures`).
- A person's private records stay separate from the organization's; the organization never reads them.
- No performance, productivity, location, screenshot or ranking functions; hints on the Working Hours Act go to the
  person only.
- Corrections keep the original value with who, when and why; closed and approved periods are locked.
- An access log that the person can see (who read or changed their data), chained by hash.
- Deletion classes with configurable retention (at least two years), legal hold, and a log of every purge.
- Export of everything stored about a person (JSON, CSV), including the history and the access log.

## What it cannot do

- Decide whether a works agreement is needed, whether a data protection impact assessment is needed, or which
  retention applies to an employer: that depends on the employer's situation.
- Replace the information of the employees (Art. 13 GDPR), the employer's decisions on leave, or the notice about
  lapsing leave; it records them.
