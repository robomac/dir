#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS_DIR="$ROOT_DIR/test_assets"

if [[ ! -d "$ASSETS_DIR" ]]; then
  echo "ERROR: test assets directory not found: $ASSETS_DIR" >&2
  exit 1
fi

if [[ -x "$ROOT_DIR/dir" ]]; then
  DIR_CMD=("$ROOT_DIR/dir")
else
  DIR_CMD=(go run .)
fi

encrypted_files=(
  "catt"
  "EncDoc.docx"
  "EncPDF.pdf"
)

run_dir() {
  local -a args=("$@")
  pushd "$ASSETS_DIR" >/dev/null
  "${DIR_CMD[@]}" -G- "${args[@]}"
  popd >/dev/null
}

assert_contains() {
  local output="$1"
  local needle="$2"
  if ! grep -Fq "$needle" <<<"$output"; then
    echo "FAIL: expected to find: $needle" >&2
    return 1
  fi
}

assert_not_contains() {
  local output="$1"
  local needle="$2"
  if grep -Fq "$needle" <<<"$output"; then
    echo "FAIL: expected to not find: $needle" >&2
    return 1
  fi
}

assert_encrypted_absent() {
  local output="$1"
  local f
  for f in "${encrypted_files[@]}"; do
    assert_not_contains "$output" "$f"
  done
}

failures=0

run_case() {
  local name="$1"
  shift

  echo "Running: $name"
  if "$@"; then
    echo "PASS: $name"
  else
    echo "FAIL: $name" >&2
    failures=$((failures + 1))
  fi
  echo
}

case_encrypted_search() {
  local rc=0
  local output
  output="$(run_dir -ti=encrypted -r -z -ct -zpw=password)"

  assert_contains "$output" "EncDoc.docx" || rc=1
  assert_contains "$output" "EncPDF.pdf" || rc=1
  assert_contains "$output" "dirhelp.txt" || rc=1
  return "$rc"
}

case_non_encrypted_dirk() {
  local rc=0
  local output
  output="$(run_dir -ti=Dirk -r -z -ct -zpw=password)"

  assert_contains "$output" "random_text.txt" || rc=1
  assert_encrypted_absent "$output" || rc=1
  return "$rc"
}

case_non_encrypted_betelgeuse() {
  local rc=0
  local output
  output="$(run_dir -ti=Betelgeuse -r -z -ct -zpw=password)"

  assert_contains "$output" "Why Dir.pptx" || rc=1
  assert_encrypted_absent "$output" || rc=1
  return "$rc"
}

case_non_encrypted_trevor() {
  local rc=0
  local output
  output="$(run_dir -ti=Trevor -r -z -ct -zpw=password)"

  assert_contains "$output" "expected_results.txt" || rc=1
  assert_encrypted_absent "$output" || rc=1
  return "$rc"
}

case_recursion_required() {
  local rc=0
  local without_r with_r
  without_r="$(run_dir -ti=RECURSE_SENTINEL_2026 -z -ct -zpw=password)"
  with_r="$(run_dir -ti=RECURSE_SENTINEL_2026 -r -z -ct -zpw=password)"

  assert_not_contains "$without_r" "recurse_only.txt" || rc=1
  assert_contains "$with_r" "recurse_only.txt" || rc=1
  assert_encrypted_absent "$with_r" || rc=1
  return "$rc"
}

run_case "encrypted-search" case_encrypted_search
run_case "non-encrypted-dirk" case_non_encrypted_dirk
run_case "non-encrypted-betelgeuse" case_non_encrypted_betelgeuse
run_case "non-encrypted-trevor" case_non_encrypted_trevor
run_case "recursion-required" case_recursion_required

if [[ "$failures" -ne 0 ]]; then
  echo "$failures case(s) failed." >&2
  exit 1
fi

echo "All verification cases passed."
