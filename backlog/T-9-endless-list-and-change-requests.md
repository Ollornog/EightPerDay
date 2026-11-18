---
id: T-9
type: Task
title: "The whole period as an endless list; change requests on approved weeks"
status: offen
milestone: M-5
tags: [ui, approval, company-features]
created: 2026-11-19
---

- A list of every week, the newest at the bottom; scrolling up loads the weeks before it (without JavaScript a link
  does the same).
- On a managed employer whose organization approves weeks, a change typed into an approved week is not refused: it
  becomes a correction (T-1) that carries the proposed values. The approved values count until the organization
  decides; the week shows the request. Approving it books the proposed values and approves the week as corrected;
  rejecting it leaves the week as it was. Both go into the member's access log.

Done when browser tests change an approved week through a request, and the endless list loads older weeks while
scrolling.
