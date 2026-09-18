#!/usr/bin/env bash
# Verify the rig's formulas (formulas/*.formula.json) are well-formed and
# behave as documented, without touching the vault's real beads database.
#
# For each formula this checks that:
#   - it is listed by `bd formula list`
#   - it cooks without error via `bd cook <name>`
#   - `bd mol pour <name> --var story=x --var title=y` creates the expected
#     step beads, with only the first step ready (per `bd ready`)
#
# All of this happens inside a throwaway beads database in a fresh temp
# dir; the vault's database is never opened.

set -euo pipefail

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FORMULAS_DIR="$REPO_ROOT/formulas"

command -v bd >/dev/null 2>&1 || fail "bd is not on PATH"
command -v jq >/dev/null 2>&1 || fail "jq is not on PATH"

[ -d "$FORMULAS_DIR" ] || fail "no $FORMULAS_DIR directory"

# formula name -> file, checked below
declare -A FORMULA_FILES=(
	[tdd-feature]="$FORMULAS_DIR/tdd-feature.formula.json"
	[chore]="$FORMULAS_DIR/chore.formula.json"
)

for name in "${!FORMULA_FILES[@]}"; do
	[ -f "${FORMULA_FILES[$name]}" ] || fail "missing formula file ${FORMULA_FILES[$name]}"
done

# Never let a vault-pointing env var leak into the throwaway database.
unset BEADS_DIR BEADS_DB BEADS_DATABASE BEADS_DB_PATH 2>/dev/null || true

TMPDIR_CHECK="$(mktemp -d "${TMPDIR:-/tmp}/check-formulas.XXXXXX")"
cleanup() {
	rm -rf "$TMPDIR_CHECK"
}
trap cleanup EXIT

cd "$TMPDIR_CHECK"
git init -q

bd init -p CHK --non-interactive --role maintainer --skip-agents --skip-hooks -q \
	|| fail "bd init failed in throwaway dir"

# Safety check: bd must be resolving to the throwaway database, not the vault.
WHERE_OUT="$(bd where 2>&1)"
case "$WHERE_OUT" in
*"$TMPDIR_CHECK"*) : ;;
*) fail "bd where does not point inside the throwaway dir; refusing to continue:
$WHERE_OUT" ;;
esac
case "$WHERE_OUT" in
*millwright-vault*) fail "bd where mentions millwright-vault; refusing to continue:
$WHERE_OUT" ;;
esac

mkdir -p .beads/formulas
cp "${FORMULA_FILES[tdd-feature]}" "${FORMULA_FILES[chore]}" .beads/formulas/

# --- bd formula list ---------------------------------------------------
LIST_JSON="$(bd formula list --json)"
for name in tdd-feature chore; do
	echo "$LIST_JSON" | jq -e --arg n "$name" 'map(.name) | index($n)' >/dev/null \
		|| fail "bd formula list does not include '$name'; got: $LIST_JSON"
done

# --- bd cook -------------------------------------------------------------
for name in tdd-feature chore; do
	bd cook "$name" >/dev/null || fail "bd cook $name exited non-zero"
done

# --- bd mol pour + bd ready ----------------------------------------------
# Expected first step id per formula, taken from formulas/*.formula.json.
declare -A FIRST_STEP_ID=(
	[tdd-feature]="understand"
	[chore]="do"
)

check_pour() {
	local name="$1" story="mw-check-story" title="Check the $1 formula"

	local pour_json
	pour_json="$(bd mol pour "$name" --var "story=$story" --var "title=$title" --json)" \
		|| fail "bd mol pour $name exited non-zero"

	local root_id expected_first_id expected_real_id
	root_id="$(echo "$pour_json" | jq -r '.new_epic_id')"
	[ -n "$root_id" ] && [ "$root_id" != "null" ] || fail "bd mol pour $name did not report a root id: $pour_json"

	expected_first_id="${FIRST_STEP_ID[$name]}"
	expected_real_id="$(echo "$pour_json" | jq -r --arg k "$name.$expected_first_id" '.id_mapping[$k]')"
	[ -n "$expected_real_id" ] && [ "$expected_real_id" != "null" ] \
		|| fail "bd mol pour $name did not create step '$expected_first_id': $pour_json"

	local ready_json
	ready_json="$(bd ready --parent "$root_id" --json)" \
		|| fail "bd ready --parent $root_id exited non-zero"

	local ready_count
	ready_count="$(echo "$ready_json" | jq 'length')"
	[ "$ready_count" = "1" ] \
		|| fail "bd ready --parent $root_id for '$name' expected exactly 1 ready step, got $ready_count: $ready_json"

	local ready_id
	ready_id="$(echo "$ready_json" | jq -r '.[0].id')"
	[ "$ready_id" = "$expected_real_id" ] \
		|| fail "'$name': ready step is $ready_id, expected first step $expected_real_id ($expected_first_id): $ready_json"
}

check_pour tdd-feature
check_pour chore

echo "OK: tdd-feature and chore formulas listed, cooked, and poured with only their first step ready"
