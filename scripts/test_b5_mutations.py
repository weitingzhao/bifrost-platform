#!/usr/bin/env python3
"""B5 §15: isolated Go overlays, baseline PASS and behavioral FAIL required.

Run from anywhere with --output outside the checkout and GOCACHE set. No
source is edited. A skipped test, compile failure or panic is never a kill.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
STORE = "internal/agentrt/store/"
CONTROL = "internal/agentrt/stgrelease/"


def mutation(name, package, tests, *edits):
    return dict(name=name, package=package, tests=tests, edits=edits)


MUTATIONS = [
    mutation("s-budget", STORE, "TestStgGateWholeRangeAndSet",
             (STORE + "stg_gate_policy.go", " || used >= 3", "")),
    mutation("direction", STORE, "TestStgGatePolicyRefusals/backwards",
             (STORE + "stg_gate_policy.go", "if parent != p {", "if false && parent != p {")),
    mutation("partial-rollout", CONTROL, "TestControlFailureEffectsAndBackoff/(this_try|all_this_try)",
             (CONTROL + "verify.go", 'class = "this_try"', 'class = "unchanged"')),
    mutation("restart-clock", CONTROL, "TestControlHealthGraceSurvivesRestart/failed",
             (CONTROL + "verify.go", 'c.now().Sub(t.RunEvidence.Record.Time("at")) < rolloutGrace', 'c.now().Sub(c.now()) < rolloutGrace')),
    mutation("block-clock", STORE, "TestB5HBlockClockSurvivesNewSnapshot",
             (STORE + "stg_release_scopes.go", 'if s.Blocks[id].IsOpen() {', 'if false && s.Blocks[id].IsOpen() {')),
    mutation("wrong-input-release", CONTROL, "TestControlWrongInputBlocksAcrossHundredScansAndGreenCIRepairs",
             (CONTROL + "verify.go", 'return c.blockAndFinish(ctx, s, id, b, "wrong_input", base, nil)', 'return c.write(ctx, s, finish(s, id, b, t.Verify.Seq, false)...)')),
    mutation("duplicate-started", STORE, "TestStgEveryEventKindHasSuccessAndDuplicateControl/started",
             (STORE + "stg_release_store.go", 'b == nil || b.Phase != "SNAP" || b.Try != r.Int("try")', 'b == nil || b.Try != r.Int("try")'),
             (STORE + "stg_release.go", 'b.Phase != "SNAP" || t.Started.Seq > 0 || e.At.Before', 'e.At.Before')),
    mutation("old-decision-time", STORE, "TestStgGatePolicyRefusals/expired_at_decision",
             (STORE + "stg_gate_policy.go", '!at.Before(r.Expires)', '!r.Effective.Before(r.Expires)')),
    mutation("mission-lock", STORE, "TestB5HBindingLocksIncludeEveryMission",
             (STORE + "stg_gate.go", 'for _, mission := range sortedStgKeys(missions) {', 'for _, mission := range []string{} {')),
    mutation("zero-replicas", "internal/releases/", "TestPodDigestsZeroReplicasFullyScaledDown",
             ("internal/releases/pods.go", ' || *dep.Spec.Replicas < 1', '')),
    mutation("task-owner-uid", "internal/delivery/", "TestSTGRunEvidenceRefusals",
             ("internal/delivery/stg_run_evidence.go", ' || owner.UID != id.UID', '')),
    mutation("late-covers", CONTROL, "TestExternalSameRunBackfillsLaterMembers",
             (CONTROL + "external.go", 'if s.Evidence[id].Seq > 0 || automaticRun(s, run) {', 'if s.Evidence[id].Seq > 0 { return errors.Join(errs...) }; if automaticRun(s, run) {')),
    mutation("unbound-404", STORE, "TestStgRecoveryTable/not_found/false",
             (STORE + "stg_gate_recovery.go", 'return "unknown_execution"', 'return "resend"')),
]


def run(case, output, overlay=None):
    args = ["go", "test", "-p", "2", "-count=1", "-json"]
    if overlay:
        args += ["-overlay", str(overlay)]
    args += ["./" + case["package"].rstrip("/"), "-run", "^" + case["tests"] + "$"]
    env = os.environ.copy()
    env.pop("AGENTRT_TEST_PG", None)
    env.pop("AGENTRT_TEST_REQUIRED", None)
    env.setdefault("GOPROXY", "off")
    with output.open("w") as log:
        result = subprocess.run(args, cwd=ROOT / "api", env=env, stdout=log,
                                stderr=subprocess.STDOUT, timeout=180)
    raw = output.read_text()
    events = []
    for line in raw.splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    failed = [e["Test"] for e in events if e.get("Action") == "fail" and "Test" in e]
    passed = [e["Test"] for e in events if e.get("Action") == "pass" and "Test" in e]
    broken = any(word in raw for word in ("[build failed]", "panic:", "no tests to run", "undefined:", "syntax error"))
    return dict(exit=result.returncode, passed=passed, failed=failed, broken=broken)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--only", help="One mutation name")
    options = parser.parse_args()
    output = options.output.resolve()
    if output == ROOT or ROOT in output.parents:
        parser.error("output must be outside the checkout")
    output.mkdir(parents=True, exist_ok=True)
    summary = []
    for case in MUTATIONS:
        if options.only and options.only != case["name"]:
            continue
        directory = output / case["name"]
        directory.mkdir(exist_ok=True)
        replacements = {}
        for index, (path, old, new) in enumerate(case["edits"]):
            original = ROOT / "api" / path
            source = original.read_text()
            if source.count(old) != 1:
                raise RuntimeError(f"{case['name']}: expected exactly one anchor in {path}")
            shadow = directory / f"{index}.go"
            shadow.write_text(source.replace(old, new, 1))
            replacements[str(original)] = str(shadow)
        overlay = directory / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replacements}, indent=2))
        baseline = run(case, directory / "baseline.jsonl")
        mutant = run(case, directory / "mutant.jsonl", overlay)
        killed = baseline["exit"] == 0 and bool(baseline["passed"]) and mutant["exit"] != 0 and bool(mutant["failed"]) and not mutant["broken"]
        summary.append(dict(name=case["name"], tests=case["tests"], killed=killed, baseline=baseline, mutant=mutant))
        print(f"{'PASS' if killed else 'FAIL'} {case['name']}: {', '.join(mutant['failed'])}", flush=True)
        (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    if not summary or not all(row["killed"] for row in summary):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
