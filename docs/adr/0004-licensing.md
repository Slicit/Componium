# ADR 0004 — AGPL-3.0 with a CLA, and a public domain spec

Status: accepted · 2026-08-29

## Context

The requirement was: others may use and share the project freely with
attribution and under the same terms, but may not exploit it commercially,
while the original author retains the right to sell it.

Read literally that describes CC BY-NC-SA. Two problems with taking it
literally:

1. **Creative Commons licences are not for software.** Creative Commons say so
   themselves. No patent grant, no obligation to distribute source, and
   incompatible with every FOSS licence, so the project could never absorb a
   GPL'd component.
2. **A non-commercial clause is not open source.** The OSI definition forbids
   discriminating against fields of endeavour. Componium would have to stop
   describing itself as open source, and would lose contributors at exactly the
   moment it most needs firmware, adapters and a shared score library.

## Decision

**AGPL-3.0, plus a contributor agreement, plus a public domain specification.**

Dual licensing achieves the actual goal without the non-commercial clause. The
project is AGPL; the copyright holder separately sells commercial licences to
anyone who does not want the AGPL's obligations. This is the model used by Qt,
MySQL, GitLab and Sidekiq.

- Share-alike and attribution are enforced on everyone.
- Commercial exploitation by others is effectively deterred: a competitor
  building a product on Componium must release their entire derived work under
  AGPL.
- The copyright holder alone can sell exemptions.
- It remains genuinely open source.

**AGPL rather than GPL** because of the composer. "Upload your film, get a
score" is the obvious hosted product someone else would build, and only the
AGPL's network clause reaches it.

**The specification is CC0**, deliberately separate. If `docs/cip.md` were
covered by the copyleft, writing a third party instrument would be legally
fraught, which defeats the point of a language agnostic protocol. Implementing
CIP must never make an implementation a derived work.

## Consequences

- **A CLA must be in place before the first external pull request is merged.**
  Dual licensing works only while one party holds sufficient rights. Merging a
  single unsigned contribution ends the commercial option permanently, and is
  not practically reversible. See [CLA/README.md](../../CLA/README.md).
- The AGPL does not prevent anyone charging money for Componium as it stands.
  It prevents them keeping their modifications closed. If the requirement ever
  hardens to "nobody but us may charge at all", the licence must change to
  something like PolyForm Noncommercial, and the project stops being open
  source at that point.
- Source files will need AGPL notice headers once code exists.
