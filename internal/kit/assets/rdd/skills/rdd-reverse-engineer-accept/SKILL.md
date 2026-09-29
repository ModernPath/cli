---
name: rdd-reverse-engineer-accept
description: Obtain and apply one human decision for an exact verified and delivered reverse-engineered baseline, or recover its existing gate and receipt. Use with a verification packet or known acceptance gate; preserves compliance approval and normal development gates.
---

# Accept verified existing behavior

## Purpose

Apply one reviewed human decision from PENDING_VERIFICATION directly to DONE.
This uses the dedicated acceptance operation, not normal development entry or
completion gates. Compliance draft/approved state is unchanged.

## Inputs and prerequisites

Read the project `AGENTS.md`, canonical `PROCESS.md` "As-built verification and
acceptance", and the consuming project's sanctioned CLI procedure. In ModernPath,
use the installed `mp-process-cli` skill and `.modernpath/cli-reference.md`.

For new acceptance, retain the verification packet: typed proof, returned proof
digest, exact pending scope, complete SR lower and independent UR upper proof,
and separate per-repository integration observations. Already DONE required SRs
are proof dependencies, not additional acceptance targets. For recovery, retain
the gate ID, exact opening/application inputs and stable keys, and any actual
human answer/source already provided. Prior permission to reverse-engineer or
implement tooling is not acceptance of the baseline.

## Resume checks

When a gate exists or an answer/application may already have succeeded, read its
status before previewing proof, opening a gate or asking the human again.

| Observed state | Next action |
|---|---|
| No existing gate, current eligible verification packet | Follow the new-acceptance steps below |
| Open gate, current proof | Reuse its stored scope and brief; obtain the answer only if the user has not already given it for this packet |
| Approved answer, current proof, no applied receipt | Apply the retained exact input; do not ask again |
| Rejected answer, no applied receipt | Report rejection and unchanged lifecycle; do not promote |
| Applied receipt | Report the historical decision, receipt and current lifecycle/compliance states. Do not reapply it or send DONE targets through baseline verification. If current required evidence is invalidated, hand that finding to `rdd-triage` |
| Pending targets with changed pins or stale proof, no applied receipt | Retain the old gate's status and return to verification for a fresh packet. Follow the consuming project's documented CLI recovery or replacement procedure before fresh human review; never reuse the old answer for changed proof |
| Status unavailable | Report the read/tooling gap; do not assume no gate exists or invent a new key |

If the opening response was lost before retaining the gate ID, retry the exact
opening input/key to recover that gate, then follow its returned state.

For recovery of the same packet, reuse the stored gate and stable retry keys.
After a lost response, retry identical input only if the status read shows that
application still needs recovery. A successor packet needs a new gate and keys
through the documented replacement procedure; changed input under an existing
key conflicts. If a required recovery or replacement operation is unsupported,
report the tooling gap through the sanctioned channel and stop that acceptance
attempt. Do not invent parameters or substitute a generic gate.

## Steps

1. **Use the verified packet.** Require complete applicable proof and the exact
   pending scope. Use the retained eligibility result when it describes this
   packet; the sanctioned opening operation rechecks it. A known gap, changed
   pin or unmerged revision returns to `rdd-reverse-engineer-verify`. Do not
   recollect unchanged evidence merely to open a decision.
2. **Open and present the decision.** Supply the typed proof, proof digest,
   stable opening key and human brief defined in `PROCESS.md`. Show the exact
   requirements becoming DONE, evidence identities, coverage limits, risk and
   recommendation, with links to relevant packet files. State that compliance
   approval is unchanged. Opening a gate is not a human answer. Present the
   current decision files before asking.
3. **Record the human answer.** Obtain one explicit attributable accept/reject
   decision for that exact packet unless it is already present in the session.
   Submit it through the existing reviewed answer mechanism with both gate and
   proof fingerprints and the USER source. The sanctioned operation rechecks
   proof here.
   Rejection ends with a report and grants no promotion.
4. **Apply and report.** Apply an approved answer through the dedicated guarded
   operation using the exact proof/gate fingerprints and a stable application
   key. It rechecks proof, atomically moves exactly the pending named scope to
   DONE, records lifecycle events, closes the applied decision and retains a
   receipt. Never use generic author/advance or fabricate RED for this edge.
   ModernPath permits an authorized applier other than the answerer; retain both
   identities in the receipt. Application cannot change the approved scope.
   Read the receipt and lifecycle/compliance states from the successful apply
   response. Do not add status/list/preview calls or replay the write merely to
   repeat that confirmation. Use a status read when the response is missing,
   incomplete or contradictory, or when recovering an interrupted operation.

## Outputs and handoff

Report the actual human answer/source, recorded versus applied state, receipt
identity, exact named scope, proof currency and unchanged compliance status.
An answer without an applied receipt is pending application, not DONE. Historical
receipts remain readable after later drift without proving current evidence.
Drift in a receipt's proof pins does not itself change lifecycle state. Route
an established invalidation through `rdd-triage`, retaining the receipt as historical evidence;
normal development resumes according to the item's entry approval and state.

After applying a human answer, report and wait as required by `PROCESS.md`,
including during the complete loop. Merge, publication and deployment need their
own authority. Missing operations go to the sanctioned tooling-gap channel,
not raw API/store writes.
