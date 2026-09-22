#!/usr/bin/env bash

set -euo pipefail

SCRIPTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="${PROJ_ROOT:-$(cd "${SCRIPTS}/.." && pwd)}"

SKILL_SOURCE="${SKILL_SOURCE:-${PROJ_ROOT}/skills}"
SKILLS_DIR="${SKILLS_DIR:-${HOME}/.claude/skills}"

main() {
	if [ ! -d "$SKILLS_DIR" ]; then
		printf "creating dir %s...\n" "$SKILLS_DIR"
		mkdir -p "$SKILLS_DIR"
	fi

	local -a installed
	mapfile -t installed < <(find "${SKILLS_DIR}" -mindepth 1 -maxdepth 1)

	for installedSkill in "${installed[@]}"; do
		if [ -L "$installedSkill" ] && [ ! -e "$installedSkill" ]; then
			rm -f "$installedSkill"
		fi
	done

	local -a skillsDir
	mapfile -t skillsDir < <(find "$SKILL_SOURCE" -mindepth 1 -maxdepth 1 -type d)

	if ((${#skillsDir[@]} == 0)); then
		printf "Error: no skill definitions were found in the directory %s\n" "$SKILL_SOURCE"
		return 1
	fi

	for skill in "${skillsDir[@]}"; do
		local name dst
		name="$(basename "$skill")"
		dst="${SKILLS_DIR}/${name}"

		rm -rf "$dst"
		cp -R "$skill" "$dst"
	done

	printf "Success: all skill(s) installed.\n"
}

main "$@"
