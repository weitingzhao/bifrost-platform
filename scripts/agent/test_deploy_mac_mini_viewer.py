#!/usr/bin/env python3
"""Ratchet: the Mini's operator plane gets the PROD viewer token, and only that.

Covers prod_viewer_export in deploy_mac_mini.sh (no SSH):
  the infra .env's PLATFORM_PROD_VIEWER_TOKEN becomes PLATFORM_VIEWER_TOKEN
  quotes and CR are stripped; an absent or empty key prints nothing
  a value equal to another token bound for the Mini is refused (exit 3)
  the Mac Pro's own PLATFORM_VIEWER_TOKEN is not in the .env sync list
"""

from __future__ import print_function

import os
import re
import subprocess
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "deploy_mac_mini.sh")


def _write(text):
    fh = tempfile.NamedTemporaryFile("w", delete=False)
    fh.write(text)
    fh.close()
    return fh.name


def export(infra_text, others_text=None):
    tmp = [_write(infra_text)]
    args = ["bash", SCRIPT, "--prod-viewer-export", tmp[0]]
    if others_text is not None:
        tmp.append(_write(others_text))
        args.append(tmp[1])
    try:
        return subprocess.run(args, capture_output=True, text=True, check=False)
    finally:
        for path in tmp:
            os.unlink(path)


class ProdViewerExportTest(unittest.TestCase):
    def test_prod_viewer_becomes_plane_viewer(self):
        proc = export("PLATFORM_PROD_OPERATOR_TOKEN=op\nPLATFORM_PROD_VIEWER_TOKEN=view-prod\n")
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "export PLATFORM_VIEWER_TOKEN=view-prod\n")

    def test_quotes_and_cr_are_stripped(self):
        proc = export('export PLATFORM_PROD_VIEWER_TOKEN="view-prod"\r\n')
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "export PLATFORM_VIEWER_TOKEN=view-prod\n")

    def test_absent_or_empty_prints_nothing(self):
        for text in ("PLATFORM_STG_VIEWER_TOKEN=stg\n", "PLATFORM_PROD_VIEWER_TOKEN=\n"):
            proc = export(text)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertEqual(proc.stdout, "")

    def test_collision_with_a_mini_token_is_refused(self):
        proc = export(
            "PLATFORM_PROD_VIEWER_TOKEN=same\n",
            "export PLATFORM_OPERATOR_TOKEN=other\nexport REMEDIATION_RUNNER_TOKEN=same\n",
        )
        self.assertEqual(proc.returncode, 3)
        self.assertEqual(proc.stdout, "")
        self.assertIn("REMEDIATION_RUNNER_TOKEN", proc.stderr)
        self.assertNotIn("same", proc.stderr.replace("REMEDIATION_RUNNER_TOKEN", ""))

    def test_distinct_tokens_pass(self):
        proc = export("PLATFORM_PROD_VIEWER_TOKEN=view-prod\n", "export PLATFORM_OPERATOR_TOKEN=op\n")
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "export PLATFORM_VIEWER_TOKEN=view-prod\n")

    def test_local_viewer_is_not_synced(self):
        with open(SCRIPT) as fh:
            text = fh.read()
        keys = re.search(r"grep -E '\^\(([A-Z_|]+)\)=' \"\$\{PLATFORM_LOCAL\}/\.env\"", text)
        self.assertIsNotNone(keys, "the .env sync key list moved; update this test")
        self.assertNotIn("PLATFORM_VIEWER_TOKEN", keys.group(1).split("|"))


if __name__ == "__main__":
    unittest.main()
