from pathlib import Path
import subprocess
import json
import unittest

from fixtures.install_workspace import InstallWorkspace


class InstallWorkspaceTests(unittest.TestCase):
    # Explicit stop uncertainty must retain real fixture files even if the
    # following status could claim no IPC owner.
    def test_retains_profile_when_stop_is_uncertain(self):
        owned = InstallWorkspace()
        self.addCleanup(self.cleanup_owned_fixture, owned.root)
        profile = owned.root/"device"
        profile.mkdir()
        (profile/"sentinel").write_text("keep")
        owned.profiles.append(profile)
        calls = []
        def cli(_profile, *words):
            calls.append(words)
            if words == ("stop",):
                return subprocess.CompletedProcess(words, 1, "", "cleanup uncertain")
            return subprocess.CompletedProcess(words, 0, '{"running":null}', "")
        owned.cli = cli
        with self.assertRaises(RuntimeError):
            with owned:
                pass
        self.assertEqual((profile/"sentinel").read_text(), "keep")
        self.assertEqual(calls, [("stop",)])
        self.assertTrue(json.loads((owned.root/"cleanup-report.json").read_text())["retained"])

    # Successful cleanup requires explicit stop success and no running owner.
    def test_removes_only_after_stop_and_status_confirmed(self):
        owned = InstallWorkspace()
        self.addCleanup(self.cleanup_owned_fixture, owned.root)
        profile = owned.root/"device"
        profile.mkdir()
        owned.profiles.append(profile)
        calls = []
        def cli(_profile, *words):
            calls.append(words)
            return subprocess.CompletedProcess(words, 0, '{"running":null}', "")
        owned.cli = cli
        with owned:
            pass
        self.assertFalse(owned.root.exists())
        self.assertEqual(calls, [("stop",), ("runtime", "status")])

    # A failed assertion keeps evidence even when every process stopped cleanly.
    def test_retains_test_failure_and_original_exception(self):
        owned = InstallWorkspace()
        self.addCleanup(self.cleanup_owned_fixture, owned.root)
        failure = AssertionError("fixture failure")
        with self.assertRaises(AssertionError) as caught:
            with owned:
                (owned.root/"evidence").write_text("keep")
                raise failure
        self.assertIs(caught.exception, failure)
        self.assertEqual((owned.root/"evidence").read_text(), "keep")
        report = json.loads((owned.root/"cleanup-report.json").read_text())
        self.assertTrue(report["test_failed"])
        self.assertEqual(report["cleanup_errors"], [])

    # One unavailable profile does not skip attempts to stop other owned owners.
    def test_attempts_all_profiles_but_retains_all_if_any_cleanup_fails(self):
        owned = InstallWorkspace()
        self.addCleanup(self.cleanup_owned_fixture, owned.root)
        for name in ("first", "second"):
            profile = owned.root/name
            profile.mkdir()
            owned.profiles.append(profile)
        calls = []
        def cli(profile, *words):
            calls.append((profile.name, words))
            if profile.name == "first":
                raise TimeoutError("fixture command deadline")
            return subprocess.CompletedProcess(words, 0, '{"running":null}', "")
        owned.cli = cli
        with self.assertRaises(RuntimeError):
            with owned:
                pass
        self.assertEqual(calls, [("first", ("stop",)), ("second", ("stop",)), ("second", ("runtime", "status"))])
        self.assertTrue(all(profile.exists() for profile in owned.profiles))

    # A corrupt/missing status cannot be turned into absence.
    def test_retains_when_exit_observation_is_invalid(self):
        for output in ('{}', 'invalid', '{"running":{"pid":123}}'):
            with self.subTest(output=output):
                owned = InstallWorkspace()
                self.addCleanup(self.cleanup_owned_fixture, owned.root)
                profile = owned.root/"device"
                profile.mkdir()
                owned.profiles.append(profile)
                owned.cli = lambda _profile, *words: subprocess.CompletedProcess(words, 0, output, "")
                with self.assertRaises(RuntimeError):
                    with owned:
                        pass
                self.assertTrue(profile.exists())

    @staticmethod
    def cleanup_owned_fixture(root):
        import shutil
        import tempfile
        assert root.parent == Path(tempfile.gettempdir()).resolve()
        assert root.name.startswith("xr-install-source-")
        if root.exists():
            shutil.rmtree(root)


if __name__ == "__main__":
    unittest.main()
