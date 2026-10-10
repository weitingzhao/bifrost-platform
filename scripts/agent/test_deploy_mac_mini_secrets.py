"""Deploy script must not copy admin credentials or a cluster kubeconfig.

The sync list is notification tokens only. Kubeconfig removal is idempotent.
"""

import re
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("deploy_mac_mini.sh")
BANNED = (
    "CURSOR_API_KEY",
    "PLATFORM_OPERATOR_TOKEN",
    "PLATFORM_ADMIN_TOKEN",
    "REMEDIATION_RUNNER_TOKEN",
    "GIT_BRIDGE_URL",
)


class DeploySecretsTests(unittest.TestCase):
    def text(self) -> str:
        return SCRIPT.read_text()

    def test_sync_list_has_no_admin_credentials(self):
        keys = re.search(
            r"grep -E '\^\(([A-Z_|]+)\)=' \"\$\{PLATFORM_LOCAL\}/\.env\"",
            self.text(),
        )
        self.assertIsNotNone(keys, "the .env sync key list moved; update this test")
        found = keys.group(1).split("|")
        for banned in BANNED:
            self.assertNotIn(banned, found)
        self.assertIn("NTFY_URL", found)
        self.assertIn("NTFY_TOPIC", found)
        self.assertIn("ALERT_RELAY_TOKEN", found)

    def test_plane_env_does_not_point_at_git_bridge(self):
        # TD-291: the bridge on the Owner's laptop answers 401 to the Minis.
        body = re.search(
            r"env\.operator-plane\.sh << 'ENVEOF'\n(.*?)\nENVEOF",
            self.text(),
            re.S,
        )
        self.assertIsNotNone(body, "the plane env heredoc moved; update this test")
        self.assertNotIn("GIT_BRIDGE_URL", body.group(1))
        self.assertNotIn(":8785", self.text())

    def test_script_does_not_scp_kubeconfig(self):
        scp_lines = [
            line for line in self.text().splitlines()
            if "scp" in line and "bifrost-k3s" in line
        ]
        self.assertEqual(scp_lines, [])
        self.assertIn("rm -f ~/.kube/bifrost-k3s.yaml", self.text())

    def test_strip_kubeconfig_is_idempotent(self):
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            kube = root / "kube"
            kube.mkdir()
            (kube / "bifrost-k3s.yaml").write_text("apiVersion: v1\n")
            (root / "env.sh").write_text(
                "export BIFROST_AGENT_ROOT=$HOME/bifrost-agent\n"
                "export KUBECONFIG=$HOME/.kube/bifrost-k3s.yaml\n"
                "export ALERT_RELAY=on\n"
            )
            for _ in range(2):
                proc = subprocess.run(
                    ["bash", str(SCRIPT), "--strip-kubeconfig", str(root)],
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertFalse((kube / "bifrost-k3s.yaml").exists())
            env = (root / "env.sh").read_text()
            self.assertNotIn("KUBECONFIG", env)
            self.assertIn("BIFROST_AGENT_ROOT", env)
            self.assertIn("ALERT_RELAY=on", env)


if __name__ == "__main__":
    unittest.main()
