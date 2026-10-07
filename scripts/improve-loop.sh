#!/usr/bin/env bash
# Works through IMPROVEMENTS.md one task at a time.
#
# Per task: an implementer model makes the change; this script runs the
# checks; a different, read-only reviewer model judges the diff; only then does
# the script tick the box and commit. Task text and checks come from HEAD, so
# the implementer cannot change what "done" means. Stops at the first task
# that fails all its attempts.
#
#   scripts/improve-loop.sh            # run until done or a failure
#   scripts/improve-loop.sh -n 3       # at most 3 tasks
#   scripts/improve-loop.sh --dry-run  # show the next task and its check
#
# Env: IMPROVE_MODEL (default opencode-go/deepseek-v4-pro), REVIEW_MODEL
#      (default sonnet), IMPROVE_ATTEMPTS (default 1), CLAUDE_BIN (default
#      claude).
# The implementer runs opencode (IMPROVE_BIN, IMPROVE_AGENT); clear
# IMPROVE_AGENT to fall back to the claude CLI. The reviewer runs the claude
# CLI unless REVIEW_AGENT names an opencode agent (then REVIEW_BIN is used).
#
# A failed check or review is retried from the committed tree, with the reason
# passed to the next attempt; each failed attempt is kept in `git stash`. Rule
# breaks (editing the backlog, committing) and blocks stop at once.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1
FILE=IMPROVEMENTS.md
SELF=scripts/improve-loop.sh
CLAUDE=${CLAUDE_BIN:-claude}
MODEL=${IMPROVE_MODEL:-opencode-go/deepseek-v4-pro}
REVIEWER=${REVIEW_MODEL:-sonnet}
IMPROVE_BIN=${IMPROVE_BIN:-opencode}
REVIEW_BIN=${REVIEW_BIN:-$CLAUDE}
IMPROVE_AGENT=${IMPROVE_AGENT:-improver}
REVIEW_AGENT=${REVIEW_AGENT:-}
LOGDIR=.verify/improve
MAX=1000
ATTEMPTS=${IMPROVE_ATTEMPTS:-1}
DRY=0

while [ $# -gt 0 ]; do
	case $1 in
	-n) MAX=$2; shift 2 ;;
	--dry-run) DRY=1; shift ;;
	*) echo "usage: $0 [-n N] [--dry-run]" >&2; exit 2 ;;
	esac
done

if [ "$MODEL" = "$REVIEWER" ]; then
	echo "✗ IMPROVE_MODEL and REVIEW_MODEL must differ" >&2
	exit 2
fi

# Fail early with a clear message if the chosen harness is not installed.
need() { command -v "$1" >/dev/null || { echo "✗ '$1' not found on PATH" >&2; exit 2; }; }
if [ -n "$IMPROVE_AGENT" ]; then need "$IMPROVE_BIN"; else need "$CLAUDE"; fi
if [ -n "$REVIEW_AGENT" ]; then need "$REVIEW_BIN"; else need "$CLAUDE"; fi

# The committed backlog: the only source of task text and checks.
spec() { git show "HEAD:$FILE"; }

is_ticked() { spec | grep -qE "^- \[x\] \*\*$1 "; }

# next_task prints the first open task that is not manual and whose
# "(needs Txx)" dependency, if any, is ticked.
next_task() {
	spec | grep -E '^- \[ \] \*\*T[0-9]+[a-z]?' | while read -r line; do
		id=$(sed -E 's/^- \[ \] \*\*(T[0-9]+[a-z]?).*/\1/' <<<"$line")
		case $line in *"(manual)"*) continue ;; esac
		dep=$(grep -oE '\(needs T[0-9]+[a-z]?\)' <<<"$line" | grep -oE 'T[0-9]+[a-z]?')
		if [ -n "$dep" ] && ! is_ticked "$dep"; then continue; fi
		echo "$id"
		return
	done
}

# task_block prints a task's text: its line up to the next task or heading.
task_block() {
	spec | awk -v id="$1" '
		$0 ~ "^- \\[[ x]\\] \\*\\*" id " " { on = 1; print; next }
		on && (/^- \[[ x]\] \*\*T[0-9]+[a-z]?/ || /^## /) { exit }
		on { print }'
}

# task_check prints the command in backticks on the task's Check line.
task_check() {
	task_block "$1" | grep -E '^\s*Check:' | head -1 | sed -E 's/^[^`]*`(.*)`[^`]*$/\1/'
}

# Changes other than the untracked files that are not ours to touch.
dirty() {
	git status --porcelain | grep -vE '^\?\? (show-me-.*\.html|\.verify/|\.archify/)'
}

# test_weight counts the tests `go test ./...` runs, minus skips, to catch
# tests that were deleted, skipped, or hidden behind a build tag. A task whose
# block has a line "Tests-tag: `tag`" may move tests behind that tag: they are
# counted with it (the reviewer judges whether the move was right).
test_weight() {
	local tags=${1:-} funcs skips
	funcs=$(go test ${tags:+-tags "$tags"} -list . ./... 2>/dev/null | grep -cE '^(Test|Fuzz|Example)')
	skips=$(grep -rhE '\bt\.Skip(Now|f)?\(' --include='*_test.go' . | wc -l)
	echo $((funcs - skips))
}

# task_tests_tag prints the build tag a task may move tests behind, if any.
task_tests_tag() {
	task_block "$1" | grep -E '^\s*Tests-tag:' | head -1 | sed -E 's/^[^`]*`([^`]*)`.*/\1/'
}

# gates runs the fixed checks and the task's committed check.
gates() {
	local check=$1
	echo "== gofmt"; test -z "$(gofmt -l .)" || { gofmt -l .; return 1; }
	echo "== build"; go build ./... || return 1
	echo "== vet";   go vet ./... || return 1
	echo "== test";  go test ./... || return 1
	echo "== check: $check"
	bash -c "$check" || return 1
}

# reviewer_run sends the review prompt on stdin and writes the reviewer's
# answer to $1. The reviewer runs the claude CLI unless REVIEW_AGENT selects an
# opencode agent, which must be read-only (e.g. researcher).
reviewer_run() {
	local out=$1
	if [ -n "$REVIEW_AGENT" ]; then
		"$REVIEW_BIN" run --model "$REVIEWER" --agent "$REVIEW_AGENT" --auto >"$out" 2>&1
	else
		"$CLAUDE" -p --model "$REVIEWER" --no-session-persistence \
			--allowedTools "Read,Glob,Grep" --disallowedTools "Edit,Write,NotebookEdit,Bash" >"$out" 2>&1
	fi
}

# implementer_run runs the implementer on the task prompt. It uses opencode
# (model $MODEL, agent $IMPROVE_AGENT, auto-approving what the agent does not
# deny) unless IMPROVE_AGENT is empty, which falls back to claude -p.
implementer_run() {
	local prompt=$1
	if [ -n "$IMPROVE_AGENT" ]; then
		"$IMPROVE_BIN" run --model "$MODEL" --agent "$IMPROVE_AGENT" --auto "$prompt"
	else
		"$CLAUDE" -p "$prompt" --model "$MODEL" \
			--allowedTools "Read,Edit,Write,Glob,Grep,Bash"
	fi
}

# review asks a different model, with read-only tools, whether the diff does
# the task. It must end with "VERDICT: PASS" or "VERDICT: FAIL: <reason>".
review() {
	local id=$1 out=$2 prompt
	git add -A -- . ':!show-me-*.html' >/dev/null
	prompt=$(
		echo "You are reviewing one change to a Go CLI repository. You may read files; do not change anything."
		echo
		echo "The task, exactly as committed:"
		task_block "$id"
		echo
		echo "Fail the change if any of these hold:"
		echo "- it does not do what the task says, or does only part of it"
		echo "- it changes things outside the task's scope"
		echo "- it makes a check pass by weakening, skipping, deleting or special-casing tests or checks"
		echo "- it is not code a careful Go reviewer would merge (errors ignored, dead code, wrong idiom)"
		echo
		echo "The diff:"
		git diff --cached
		echo
		echo "End your answer with exactly one line: VERDICT: PASS  or  VERDICT: FAIL: <one-line reason>"
	)
	printf '%s\n' "$prompt" | reviewer_run "$out"
	git reset -q
}

mkdir -p "$LOGDIR"
done_count=0
while [ "$done_count" -lt "$MAX" ]; do
	id=$(next_task)
	if [ -z "$id" ]; then
		echo "No open tasks left (manual tasks and tasks waiting on them are skipped)."
		exit 0
	fi
	check=$(task_check "$id")
	title=$(task_block "$id" | head -1 | sed -E 's/^- \[ \] \*\*//; s/\*\*.*//')
	echo "▶ $title"
	echo "  check: $check"
	if [ "$DRY" = 1 ]; then exit 0; fi
	if [ -z "$check" ]; then
		echo "✗ $id has no check command in backticks; fix $FILE and commit it" >&2
		exit 1
	fi
	if [ -n "$(dirty)" ]; then
		echo "✗ working tree not clean; commit or stash first:" >&2
		dirty >&2
		exit 1
	fi

	log="$LOGDIR/$id.log"
	verdict="$LOGDIR/$id.review"
	: >"$log"
	head_before=$(git rev-parse HEAD)
	tests_tag=$(task_tests_tag "$id")
	weight_before=$(test_weight "$tests_tag")

	# stop ends the run for problems a retry must not paper over.
	stop() {
		echo "✗ $id: $1" >&2
		echo "  log: $log    inspect: git diff / git status / git stash list" >&2
		exit 1
	}

	# set_aside stashes a failed attempt (kept, never deleted) so the next
	# attempt starts from the committed tree.
	set_aside() {
		git stash push -q -u -m "improve: $id attempt $1 ($MODEL) failed: $2" -- . ':!show-me-*.html' ||
			stop "could not stash attempt $1"
	}

	feedback=""
	passed=0
	for attempt in $(seq 1 "$ATTEMPTS"); do
		rm -f "$LOGDIR/$id.blocked"
		echo "  attempt $attempt/$ATTEMPTS ($MODEL)"

		# 1. Implement.
		prompt="Read $FILE and follow the rules for the implementer. Do task $id only. Do not edit $FILE or $SELF, do not tick, stage or commit. Stop when the task's Check and go test pass."
		if [ -n "$feedback" ]; then
			prompt="$prompt

A previous attempt at this task was rejected and discarded; you start from the committed tree. Why it was rejected:
$feedback"
		fi
		echo "== attempt $attempt: implementer ($MODEL)" >>"$log"
		implementer_run "$prompt" >>"$log" 2>&1 ||
			stop "the implementer run failed"

		# 2. Rule breaks and blocks end the run; no retry.
		[ -f "$LOGDIR/$id.blocked" ] && stop "implementer blocked: $(head -1 "$LOGDIR/$id.blocked")"
		[ "$(git rev-parse HEAD)" = "$head_before" ] || stop "the implementer committed; only the script commits"
		git diff --quiet HEAD -- "$FILE" "$SELF" || stop "the implementer edited $FILE or $SELF"

		# 3. Mechanical checks, against the committed spec. Failures are retried.
		reason=""
		gate_out="$LOGDIR/$id.gates"
		if [ -z "$(dirty)" ]; then
			reason="no changes were made"
		elif [ "$(test_weight "$tests_tag")" -lt "$weight_before" ]; then
			reason="fewer tests run under 'go test ${tests_tag:+-tags $tests_tag }./...' than before ($(test_weight "$tests_tag") < $weight_before): tests were removed, skipped, or hidden behind a build tag"
		elif ! gates "$check" >"$gate_out" 2>&1; then
			reason="checks failed; last lines of their output:
$(tail -40 "$gate_out")"
		fi
		cat "$gate_out" >>"$log" 2>/dev/null

		# 4. Independent review by a different model.
		if [ -z "$reason" ]; then
			echo "  reviewing ($REVIEWER)…"
			review "$id" "$verdict"
			git diff --quiet HEAD -- "$FILE" "$SELF" || stop "the review changed files"
			line=$(grep -E '^VERDICT: ' "$verdict" | tail -1)
			case $line in
			"VERDICT: PASS") passed=1; break ;;
			"VERDICT: FAIL"*) reason="the reviewer rejected it: ${line#VERDICT: FAIL: }
Review:
$(tail -40 "$verdict")" ;;
			*) reason="the reviewer gave no verdict" ;;
			esac
		fi

		echo "  ✗ attempt $attempt: $(head -1 <<<"$reason")"
		echo "$reason" >>"$log"
		set_aside "$attempt" "$(head -1 <<<"$reason")"
		feedback=$reason
	done
	[ "$passed" = 1 ] || stop "failed $ATTEMPTS attempts; each is kept in git stash"

	# 4. Tick and commit: the only writes to the backlog.
	sed -i.bak -E "s/^- \[ \] \*\*$id /- [x] **$id /" "$FILE" && rm -f "$FILE.bak"
	git add -A -- . ':!show-me-*.html'
	git commit -q -m "improve: $title" -m "Implemented by $MODEL; checks: $check; reviewed by $REVIEWER." ||
		stop "commit failed"
	echo "✓ $id  $(git log -1 --format=%h) (reviewed by $REVIEWER)"
	done_count=$((done_count + 1))
done
echo "Stopped after $done_count task(s) (-n $MAX)."
