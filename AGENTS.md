# AGENTS.md

Guidance for AI agents working in this repo.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues, operated via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical roles, label string equals role name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Regression evidence

SPEC's list of evidence every stage must leave behind lives as code in
`internal/evidence`. Adding or renaming a test that serves one of those
requirements means updating that ledger too — its own test fails otherwise,
which is the point.

