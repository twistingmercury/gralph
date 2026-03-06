#!/usr/bin/env bash
#
# ralph.sh — Drives a loop of claude-code iterations against a PRD checklist.
#
# Usage:
#   ./ralph.sh
#
# Environment variables:
#   MAX_ATTEMPTS   Maximum consecutive attempts on a single item before
#                  abandoning it (default: 10)
#   RALPH_DIR      Directory containing PRD.md, PROMPT.md, and progress.txt
#                  (default: <repo-root>/.claude/ralph)
#
# Invokes claude with --print (non-interactive) and --dangerously-skip-permissions.
#
# Exit codes:
#   0  All items complete (or no items to begin with)
#   1  PRD.md or PROMPT.md not found

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
RALPH_DIR="${RALPH_DIR:-${SCRIPT_DIR}}"
readonly RALPH_DIR
readonly PRD_FILE="${RALPH_DIR}/PRD.md"
readonly PROMPT_FILE="${RALPH_DIR}/PROMPT.md"
readonly PROGRESS_FILE="${RALPH_DIR}/progress.txt"
readonly MAX_ATTEMPTS="${MAX_ATTEMPTS:-10}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

log() {
    printf '[ralph] %s\n' "$*"
}

# Print an error message to stderr and exit 1.
die() {
    printf '[ralph] ERROR: %s\n' "$*" >&2
    exit 1
}

# Return the full text of the first unchecked item line, or empty string.
first_open_item() {
    grep -m 1 '^- \[ \]' "${PRD_FILE}" 2>/dev/null || true
}

# Replace the first `- [ ]` line in PRD.md with `- [~]` (abandoned).
# Uses perl -i for BSD/macOS compatibility.
abandon_first_open_item() {
    perl -i -0pe 's/^- \[ \]/- [~]/m' "${PRD_FILE}"
}

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

validate_inputs() {
    [ -f "${PRD_FILE}" ]    || die "PRD.md not found: ${PRD_FILE}"
    [ -f "${PROMPT_FILE}" ] || die "PROMPT.md not found: ${PROMPT_FILE}"
    [ -f "${PROGRESS_FILE}" ] || touch "${PROGRESS_FILE}"
}

# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------

main() {
    validate_inputs

    log "Starting loop (MAX_ATTEMPTS=${MAX_ATTEMPTS})"

    local current_item=""
    local attempt=0

    while true; do
        local item
        item="$(first_open_item)"

        # No open items left — we are done.
        if [ -z "${item}" ]; then
            log "All items complete."
            return 0
        fi

        # Detect whether the tracked item changed between iterations.
        if [ "${item}" != "${current_item}" ]; then
            current_item="${item}"
            attempt=1
        else
            attempt=$(( attempt + 1 ))
        fi

        log "Current item: ${current_item} (attempt ${attempt}/${MAX_ATTEMPTS})"
        log "Step 1/4: Reading PRD and progress..."
        log "Step 2/4: Invoking claude-code..."

        # Run one claude-code iteration, injecting runtime file paths.
        # Allow non-zero exit — a crash counts as a failed attempt, not a fatal error.
        { cat "${PROMPT_FILE}"
          printf '\n## Runtime paths\n- PRD: %s\n- Progress: %s\n' "${PRD_FILE}" "${PROGRESS_FILE}"
        } | claude --print --dangerously-skip-permissions || log "claude exited non-zero — counting as failed attempt."

        log "Step 3/4: Checking result..."

        # Re-read to check whether the item was completed.
        local item_after
        item_after="$(first_open_item)"

        # Item was completed or replaced by a different open item.
        if [ "${item_after}" != "${current_item}" ]; then
            log "Step 4/4: Item completed."
            current_item=""
            attempt=0
            continue
        fi

        # Item still open — check attempt limit.
        if [ "${attempt}" -ge "${MAX_ATTEMPTS}" ]; then
            log "Step 4/4: Attempt limit reached. Abandoning: ${current_item}"
            abandon_first_open_item
            current_item=""
            attempt=0
            continue
        fi

        log "Step 4/4: Item not yet complete (attempt ${attempt}/${MAX_ATTEMPTS})."
    done
}

main "$@"