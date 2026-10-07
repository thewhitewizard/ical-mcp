# iCal MCP

A read-only MCP server that gives an AI assistant access to the user's calendars, read from iCal feeds served over HTTPS.

## Language

**Calendar**:
A named source of events, identified by its configured alias (e.g. "perso", "travail").
_Avoid_: Agenda, source

**Feed**:
The secret HTTPS address of a Calendar's iCal data. Never exposed to the assistant, in results, errors or logs.
_Avoid_: URL, link, address (when talking about what the assistant sees)

**Event**:
A single or recurring item in a Calendar, identified by its `uid`.
_Avoid_: Appointment, meeting

**Occurrence**:
One dated instance of an Event (the Event itself if it does not recur). Identified by `uid` plus start; this is what `get_event` returns.
_Avoid_: Instance, entry

**Window**:
The inclusive range of days `from`–`to`, cut in the configured timezone, in which Occurrences are listed. An Occurrence that overlaps the Window is in it.
_Avoid_: Period, range (reserved for the `max_range_days` limit)

**Stale**:
Said of a Calendar served from cache because its Feed could not be downloaded.
_Avoid_: Outdated, expired
