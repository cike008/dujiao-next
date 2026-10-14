import importlib.util
import pathlib
import unittest

import yaml

path = pathlib.Path(__file__).resolve().parents[1] / "prepare-reseller-enable.py"
spec = importlib.util.spec_from_file_location("reseller_enable", path)
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
HOSTS = ["share.aimosh.com", "moshskmgr.aimosh.com", "localhost", "127.0.0.1", "::1", "dujiaonext"]


class EnableTests(unittest.TestCase):
    def test_preserves_all_unrelated_secrets_and_settings(self):
        source = "app:\n  secret_key: fixture-secret\nqueue:\n  enabled: true\nreseller:\n  enabled: false\n  settlement_confirm_days: 14\n  main_hosts: [legacy.example.com]\n"
        result, summary = helper.prepare(source, HOSTS, [])
        before, after = yaml.safe_load(source), yaml.safe_load(result)
        self.assertEqual(after["app"], before["app"])
        self.assertEqual(after["queue"], before["queue"])
        self.assertIn("legacy.example.com", after["reseller"]["main_hosts"])
        self.assertEqual(summary["settlement_confirm_days"], 14)
        self.assertNotIn("fixture-secret", str(summary))
        self.assertFalse(after["reseller"]["trusted_forwarded_host"])

    def test_rejects_duplicate_keys_and_invalid_switch(self):
        for source in ["reseller: {}\nreseller: {}\n", "reseller:\n  enabled: true\n", "reseller:\n  enabled: 'false'\n", "reseller: []\n"]:
            with self.assertRaises(ValueError):
                helper.prepare(source, HOSTS, [])

    def test_requires_internal_and_main_hosts(self):
        with self.assertRaises(ValueError):
            helper.prepare("app: {}", HOSTS[:-1], [])

    def test_rejects_env_override_without_printing_secret_values(self):
        with self.assertRaises(ValueError) as error:
            helper.prepare("app: {}", HOSTS, ["RESELLER_ENABLED=false", "SECRET=fixture-secret"])
        self.assertNotIn("fixture-secret", str(error.exception))

    def test_rejects_unsafe_hosts(self):
        for host in ["https://example.com", "*.aimosh.com", "example.com:443", "x;return 200", "-x.example.com"]:
            with self.assertRaises(ValueError):
                helper.prepare("app: {}", HOSTS + [host], [])


if __name__ == "__main__":
    unittest.main()
