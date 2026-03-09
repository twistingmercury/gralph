# Design: Gralph PRD Template

**Date:** 2026-03-06
**Status:** Approved

## Problem

There is no reusable starting point for writing PRDs that drive gralph loops. Authors must either copy the existing `scripts/PRD.md` and manually strip it, or write from scratch and risk missing required fields.

## Goal

Create an opinionated, annotated PRD template at `docs/templates/PRD-template.md` that authors can copy and fill in for any new gralph-driven project.

## Audience

Teams using gralph to drive ralph loops against a PRD checklist.

## Approach

**Guided scaffold with inline examples (Option B).** Each section includes an HTML comment explaining intent plus an italicized or blockquoted example. Self-teaching without being verbose.

## Template Structure

### Top matter
- Title placeholder
- One-line reminder that gralph processes cycles top-to-bottom
- Note that `- [ ]`, `- [x]`, `- [~]` markers are meaningful to gralph

### Narrative sections
Each has a comment explaining purpose and an inline example:

| Section | Comment focus | Example style |
|---|---|---|
| Objective | What the deliverable is | One-sentence italicized example |
| Problem Statement | Why this work is needed | 2-3 sentence example |
| Success Criteria | Observable, testable outcomes | Bulleted list example |
| Scope — In | What gralph cycles will implement | 3 bullet examples |
| Scope — Out | What to explicitly exclude | 3 bullet examples |
| Constraints and Decisions | Non-negotiable technical choices Claude must respect | 3 bullet examples |

### Implementation Plan
- Top-level comment explaining cycle sizing (small, independently verifiable, one agent)
- Two cycles included:
  - **Cycle 1**: fully worked example with realistic placeholder text
  - **Cycle 2**: bare scaffold with per-field inline comments
- Per-field comments:
  - `Agent` — lists valid agent names from the CLAUDE.md delegation table
  - `Files` — list only files this cycle touches
  - `Steps` — atomic, grep-friendly actions
  - `Verify` — must be a runnable command, not a description
  - `Done` — observable exit condition, not a restatement of steps

### Risks and Mitigations
- Comment flags the most common gralph failure mode: cycles that loop indefinitely
- 2 worked `Risk / Mitigation` examples

### Definition of Done
- Comment explains this is the global exit condition for the entire loop
- 3-4 concrete, verifiable example statements

## File location

`docs/templates/PRD-template.md`

## Out of scope

- PROMPT.md template (separate concern)
- Automation to scaffold from the template
- Validation tooling
