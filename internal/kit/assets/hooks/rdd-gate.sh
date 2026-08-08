#!/bin/sh
# ModernPath process gate (REQ-CROSS-030).
#
# Blocks a commit whose ledgers or epic records break a rule that must hold.
# Instructions are context, not configuration — an agent reads them, usually
# follows them, and the times it does not look exactly like the times it does.
# This is the part that cannot be talked out of.
#
# Only `git commit` is gated: the check is cheap, but scanning the repository on
# every shell command would tax the loop for no benefit.
#
# It fails OPEN on tooling problems and CLOSED only on real violations. A gate
# that blocks every commit because the CLI is old, or python is missing, gets
# switched off within the hour — and then it protects nothing at all.

CMD=$(cat | python3 -c 'import json,sys; print((json.load(sys.stdin).get("tool_input") or {}).get("command",""))' 2>/dev/null)

case "$CMD" in
  *"git commit"*) ;;
  *) printf '{}'; exit 0 ;;
esac

OUT=$(modernpath check 2>&1)
if [ $? -eq 0 ]; then printf '{}'; exit 0; fi

# Only a real violation report denies. Anything else — command not found, an
# unknown subcommand from an older CLI, a crash — passes through.
case "$OUT" in
  *"process violation(s)"*) ;;
  *) printf '{}'; exit 0 ;;
esac

REASON=$(printf '%s' "$OUT" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null)
[ -z "$REASON" ] && REASON='"process check failed"'
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":%s}}' "$REASON"
