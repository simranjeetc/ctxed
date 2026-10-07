#!/usr/bin/env bash
# Works through IMPROVEMENTS.md one task at a time.
#
# Per task: an implementer model makes the change; this script runs the
# checks; a different, read-only reviewer model judges the diff; only then does
# the script tick the box and commit. Task text and checks come from HEAD, so
# the implementer cannot change what "done" means. Stops at the first failure.
#
#   scripts/improve-loop.sh            # run until done or a failure
#   scripts/improve-loop.sh -n 3       # at most 3 tasks
#   scripts/improve-loop.sh --dry-run  # show the next task and its check
#
# Env: IMPROVE_MODEL (default haiku), REVIEW_MODEL (default sonnet),
#      CLAUDE_BIN (default claude).
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1
FILE=IMPROVEMENTS.md
SELF=scripts/improve-loop.sh
MODEL=${IMPROVE_MODEL:-haiku}
REVIEWER=${REVIEW_MODEL:-sonnet}
CLAUDE=${CLAUDE_BIN:-claude}
LOGDIR=.verify/improve
MAX=1000
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

# The committed backlog: the only source of task text and checks.
spec() { git show "HEAD:$FILE"; }

is_ticked() { spec | grep -qE "^- \[x\] \*\*$1 "; }

# next_task prints the first open task that is not manual and whose
# "(needs Txx)" dependency, if any, is ticked.
next_task() {
	spec | grep -E '^- \[ \] \*\*T[0-9]+' | while read -r line; do
		id=$(sed -E 's/^- \[ \] \*\*(T[0-9]+).*/\1/' <<<"$line")
		case $line in *"(manual)"*) continue ;; esac
		dep=$(grep -oE '\(needs T[0-9]+\)' <<<"$line" | grep -oE 'T[0-9]+')
		if [ -n "$dep" ] && ! is_ticked "$dep"; then continue; fi
		echo "$id"
		return
	done
}

# task_block prints a task's text: its line up to the next task or heading.
task_block() {
	spec | awk -v id="$1" '
		$0 ~ "^- \\[[ x]\\] \\*\\*" id " " { on = 1; print; next }
		on && (/^- \[[ x]\] \*\*T[0-9]+/ || /^## /) { exit }
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

# test_weight counts test functions minus skips, to catch deleted or skipped tests.
test_weight() {
	local funcs skips
	funcs=$(grep -rhE '^func (Test|Fuzz|Benchmark)' --include='*_test.go' . | wc -l)
	skips=$(grep -rhE '\bt\.Skip(Now|f)?\(' --include='*_test.go' . | wc -l)
	echo $((funcs - skips))
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

# review asks a different model, with read-only tools, whether the diff does
# the task. It must end with "VERDICT: PASS" or "VERDICT: FAIL: <reason>".
review() {
	local id=$1 out=$2
	git add -A -- . ':!show-me-*.html' >/dev/null
	{
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
	} | "$CLAUDE" -p --model "$REVIEWER" --no-session-persistence \
		--allowedTools "Read,Glob,Grep" --disallowedTools "Edit,Write,NotebookEdit,Bash" >"$out" 2>&1
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
	rm -f "$LOGDIR/$id.blocked"
	: >"$log"
	head_before=$(git rev-parse HEAD)
	weight_before=$(test_weight)
	undo="git reset --hard $head_before && git clean -fd -e show-me-*.html -e .verify -e .archify"

	stop() {
		echo "✗ $id: $1" >&2
		echo "  log: $log    review: git diff / git status    undo: $undo" >&2
		exit 1
	}

	# 1. Implement.
	prompt="Read $FILE and follow the rules for the implementer. Do task $id only. Do not edit $FILE or $SELF, do not tick, stage or commit. Stop when the task's Check and go test pass."
	echo "== implementer ($MODEL)" >>"$log"
	"$CLAUDE" -p "$prompt" --model "$MODEL" \
		--allowedTools "Read,Edit,Write,Glob,Grep,Bash" >>"$log" 2>&1 ||
		stop "the implementer run failed"

	# 2. Mechanical checks, against the committed spec.
	[ -f "$LOGDIR/$id.blocked" ] && stop "implementer blocked: $(head -1 "$LOGDIR/$id.blocked")"
	[ "$(git rev-parse HEAD)" = "$head_before" ] || stop "the implementer committed; only the script commits"
	git diff --quiet HEAD -- "$FILE" "$SELF" || stop "the implementer edited $FILE or $SELF"
	[ -n "$(dirty)" ] || stop "no changes made"
	[ "$(test_weight)" -ge "$weight_before" ] || stop "tests were removed or skipped"
	gates "$check" >>"$log" 2>&1 || stop "checks failed (gofmt/build/vet/test or the task check)"

	# 3. Independent review by a different model.
	echo "  reviewing ($REVIEWER)…"
	review "$id" "$verdict"
	git diff --quiet HEAD -- "$FILE" "$SELF" || stop "the review changed files"
	line=$(grep -E '^VERDICT: ' "$verdict" | tail -1)
	case $line in
	"VERDICT: PASS") ;;
	"VERDICT: FAIL"*) stop "reviewer: ${line#VERDICT: FAIL: } (see $verdict)" ;;
	*) stop "reviewer gave no verdict (see $verdict)" ;;
	esac

	# 4. Tick and commit: the only writes to the backlog.
	sed -i.bak -E "s/^- \[ \] \*\*$id /- [x] **$id /" "$FILE" && rm -f "$FILE.bak"
	git add -A -- . ':!show-me-*.html'
	git commit -q -m "improve: $title" -m "Implemented by $MODEL; checks: $check; reviewed by $REVIEWER." ||
		stop "commit failed"
	echo "✓ $id  $(git log -1 --format=%h) (reviewed by $REVIEWER)"
	done_count=$((done_count + 1))
done
echo "Stopped after $done_count task(s) (-n $MAX)."
