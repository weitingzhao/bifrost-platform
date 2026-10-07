#!/usr/bin/env python3
"""Ratchet: deploy_mac_mini.sh must not silently turn the alert relay off.

Covers the config generator only (no SSH):
  unset deployer env keeps a remote ALERT_RELAY=on
  --disable-alert-relay writes off and warns
  no remote file uses the default and warns
"""

from __future__ import print_function

import os
import subprocess
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "deploy_mac_mini.sh")
PEER_URL = "http://192.168.10.50:8783/api/v1/alerts/relay"


def _base_env(extra=None):
    env = os.environ.copy()
    env.pop("ALERT_RELAY", None)
    env.pop("PEER_RELAY_URL", None)
    if extra:
        env.update(extra)
    return env


def resolve(plane_text, local_text, extra_env=None, disable=False, no_plane=False, no_local=False):
    """Run the generator. plane_text/local_text None means no remote file."""
    args = ["bash", SCRIPT, "--resolve-relay-config"]
    tmp = []
    try:
        if no_plane or plane_text is None:
            args.append("--no-plane-file")
        else:
            fh = tempfile.NamedTemporaryFile("w", delete=False)
            fh.write(plane_text)
            fh.close()
            tmp.append(fh.name)
            args.extend(["--plane-file", fh.name])
        if no_local or local_text is None:
            args.append("--no-local-file")
        else:
            fh = tempfile.NamedTemporaryFile("w", delete=False)
            fh.write(local_text)
            fh.close()
            tmp.append(fh.name)
            args.extend(["--local-file", fh.name])
        if disable:
            args.append("--disable-alert-relay")
        proc = subprocess.run(
            args,
            env=_base_env(extra_env),
            capture_output=True,
            text=True,
            check=False,
        )
    finally:
        for path in tmp:
            try:
                os.unlink(path)
            except OSError:
                pass
    values = {}
    for line in proc.stdout.splitlines():
        if "=" not in line:
            continue
        key, val = line.split("=", 1)
        values[key] = val
    return proc.returncode, values, proc.stdout, proc.stderr


class ResolveRelayConfigTest(unittest.TestCase):
    def test_unset_keeps_remote_on(self):
        code, values, _stdout, stderr = resolve(
            "export ALERT_RELAY=on\n",
            "export PEER_RELAY_URL=%s\n" % PEER_URL,
        )
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "on")
        self.assertEqual(values.get("PEER_RELAY_URL"), PEER_URL)
        self.assertNotIn("defaulting ALERT_RELAY=off", stderr)
        self.assertNotIn("turning ALERT_RELAY OFF", stderr)

    def test_unset_keeps_quoted_on(self):
        code, values, _stdout, stderr = resolve(
            'export ALERT_RELAY="on"\n',
            "export PEER_RELAY_URL=%s\n" % PEER_URL,
        )
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "on")
        self.assertNotIn("defaulting ALERT_RELAY=off", stderr)

    def test_explicit_disable_writes_off_and_warns(self):
        code, values, _stdout, stderr = resolve(
            "export ALERT_RELAY=on\n",
            "export PEER_RELAY_URL=%s\n" % PEER_URL,
            disable=True,
        )
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "off")
        self.assertEqual(values.get("PEER_RELAY_URL"), PEER_URL)
        self.assertIn("WARNING", stderr)
        self.assertIn("--disable-alert-relay", stderr)
        self.assertIn("turning ALERT_RELAY OFF", stderr)

    def test_explicit_off_without_flag_keeps_on(self):
        code, values, _stdout, stderr = resolve(
            "export ALERT_RELAY=on\n",
            "export PEER_RELAY_URL=%s\n" % PEER_URL,
            extra_env={"ALERT_RELAY": "off"},
        )
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "on")
        self.assertIn("WARNING", stderr)
        self.assertIn("--disable-alert-relay", stderr)
        self.assertNotIn("turning ALERT_RELAY OFF", stderr)

    def test_no_remote_file_defaults_and_warns(self):
        code, values, stdout, stderr = resolve(None, None, no_plane=True, no_local=True)
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "off")
        self.assertEqual(values.get("PEER_RELAY_URL"), "")
        self.assertIn("WARNING", stderr)
        self.assertIn("defaulting ALERT_RELAY=off", stderr)
        self.assertIn("defaulting PEER_RELAY_URL to empty", stderr)
        self.assertEqual(
            [line for line in stdout.splitlines() if line],
            ["ALERT_RELAY=off", "PEER_RELAY_URL="],
        )

    def test_explicit_on_and_explicit_peer_url(self):
        new_url = "http://192.168.10.50:8783/api/v1/alerts/relay"
        code, values, _stdout, stderr = resolve(
            "export ALERT_RELAY=off\n",
            "export PEER_RELAY_URL=\n",
            extra_env={"ALERT_RELAY": "on", "PEER_RELAY_URL": new_url},
        )
        self.assertEqual(code, 0)
        self.assertEqual(values.get("ALERT_RELAY"), "on")
        self.assertEqual(values.get("PEER_RELAY_URL"), new_url)
        self.assertNotIn("turning ALERT_RELAY OFF", stderr)


if __name__ == "__main__":
    unittest.main()
