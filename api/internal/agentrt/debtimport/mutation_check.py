#!/usr/bin/env python3
"""Seed each review regression in an isolated disposable package copy."""
import os
import pathlib
import shutil
import subprocess
import tempfile

PACKAGE = pathlib.Path(__file__).resolve().parent
# TestFullLedgers also has to fail for value-level mutations when the frozen
# sources are supplied (DEBTIMPORT_LEDGER / DEBTIMPORT_RATCHETS, absolute paths).
FULL = ["TestFullLedgers"] if os.environ.get("DEBTIMPORT_LEDGER") and os.environ.get("DEBTIMPORT_RATCHETS") else []
MUTATIONS = [
    ("force cost S", "approvals.go", "d.Cost = f.value", 'd.Cost = "S"', ["TestApprovalCostsSurviveParsing"]),
    ("impact becomes note", "parse.go", '"影响": "impact"', '"影响": "note"', ["TestImpactMapsToImpact"]),
    ("drop commit evidence", "fields.go", "for _, sha := range commits {", "for _, sha := range []string{} {", ["TestEvidenceCommitIsStructured"]),
    ("ignore acceptance date", "fields.go", 'if date := fullDate.FindString(raw); date != "" {', 'if date := fullDate.FindString(raw); false {', ["TestAcceptanceDateDrivesRanAt"]),
    ("stop med normalization", "approvals.go", 'd.Risk = "medium"', 'd.Risk = "med"', ["TestRiskMedNormalizes"]),
    ("drop decision member refs", "sections.go", 'p.req.Refs = append(p.req.Refs, Ref{DebtID: id, RefKind: "waits_on_owner", DebtDecisionID: n, Covers: "start"})', '_ = n', ["TestDecisionMembersHaveStartReferences"]),
    ("lower-case area", "parse.go", "d.Area = strings.TrimSpace(l.text[m[4]:m[5]])", "d.Area = strings.ToLower(strings.TrimSpace(l.text[m[4]:m[5]]))", ["TestAreaAndTitleKeepSourceCase"] + FULL),
    ("English Impact becomes note", "parse.go", '"Impact": "impact"', '"Impact": "note"', ["TestSectionThreeKeysKeepTheirKinds"] + FULL),
    ("English Ratchet becomes note", "parse.go", '"Ratchet": "ratchet_plan"', '"Ratchet": "note"', ["TestSectionThreeKeysKeepTheirKinds"] + FULL),
]


def run(directory, test):
    return subprocess.run(
        ["go", "test", "./internal/agentrt/debtimport", "-count=1", "-run", "^(" + test + ")$"],
        cwd=directory, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False,
    )


def main():
    with tempfile.TemporaryDirectory(prefix="debtimport-mutations-") as tmp:
        module = pathlib.Path(tmp)
        target = module / "internal/agentrt/debtimport"
        shutil.copytree(PACKAGE, target)
        command = module / "cmd/debtimport"
        command.mkdir(parents=True)
        shutil.copyfile(PACKAGE.parents[2] / "cmd/debtimport/projects.json", command / "projects.json")
        (module / "go.mod").write_text("module github.com/weitingzhao/bifrost-platform/api\n\ngo 1.23.0\n")
        tests = sorted({test for mutation in MUTATIONS for test in mutation[4]})
        baseline = run(module, "|".join(tests))
        if baseline.returncode:
            raise SystemExit("Baseline failed:\n" + baseline.stdout)
        for label, filename, before, after, named in MUTATIONS:
            file = target / filename
            original = file.read_text()
            if original.count(before) != 1:
                raise SystemExit("Mutation anchor is not unique: " + label)
            try:
                file.write_text(original.replace(before, after, 1))
                for test in named:
                    result = run(module, test)
                    if not result.returncode or "--- FAIL: " + test not in result.stdout:
                        raise SystemExit("Mutation survived or failed to compile: " + label + "\n" + result.stdout)
                    print(label + " -> " + test + " FAIL (expected)", flush=True)
            finally:
                file.write_text(original)
    print("%d/%d mutations killed; production sources unchanged" % (len(MUTATIONS), len(MUTATIONS)))


if __name__ == "__main__":
    main()
