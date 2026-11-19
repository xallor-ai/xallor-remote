import copy
import unittest

from tests.fixtures.mcp_consent_evidence import CASE, CASES, PACKAGE, RESUME, REVOKE, ROTATE, SCENARIOS, validate


class MCPConsentEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.events = [{"Package": PACKAGE, "Test": CASE, "Action": action} for action in ("run", "pass")]
        self.report = {"passed": True, "profile_root_removed": True, "cleanup_errors": [], "signed_release": False,
                       "cases": sorted(CASES), "launches": [dict(pid=pid, kernel_exit=True, reaped=True, endpoint_absent=True) for pid in (1, 2)],
                       "observations": {"mcp_transport": "real Node child stdio JSON-RPC", "approved_scope": ["exec", "info", "read"],
                                        "jobs_after_terminal": 0, "mcp_processes": [dict(pid=pid, reaped=True, cleanup_confirmed=True) for pid in (3, 4)]}}

    # Go exit0 alone is insufficient: missing inputs skip this optional test.
    def test_rejects_skip_and_wrong_case_despite_zero_driver_exit(self):
        for events in ([{"Package": PACKAGE, "Test": CASE, "Action": "skip"}],
                       [{"Package": PACKAGE, "Test": "OtherCase", "Action": "pass"}], []):
            with self.subTest(events=events), self.assertRaises(ValueError):
                validate(events, self.report)

    # A green test body must not hide a failed or retained process/profile.
    def test_rejects_cleanup_or_process_identity_gaps(self):
        for mutate in (lambda r: r.update(profile_root_removed=False),
                       lambda r: r["observations"]["mcp_processes"][0].update(cleanup_confirmed=False),
                       lambda r: r["launches"][0].update(endpoint_absent=False),
                       lambda r: r["observations"]["mcp_processes"][0].update(pid=1)):
            report = copy.deepcopy(self.report)
            mutate(report)
            with self.assertRaises(ValueError):
                validate(self.events, report)

    # Removing scope/refusal coverage cannot be replaced by duplicate case names.
    def test_rejects_partial_or_duplicated_coverage(self):
        for cases in (sorted(CASES)[:-1], sorted(CASES)[:-1] + [sorted(CASES)[0]]):
            report = dict(self.report, cases=cases)
            with self.assertRaises(ValueError):
                validate(self.events, report)

    # The complete evidence shape is accepted; signaled teardown is permitted.
    def test_accepts_complete_native_stdio_evidence(self):
        self.report["observations"]["mcp_processes"][0]["signaled"] = True
        validate(self.events, self.report)

    def extended_report(self, case):
        report = copy.deepcopy(self.report)
        report["cases"] = sorted(SCENARIOS[case][0])
        obs = report["observations"]
        obs["approved_scope"] = SCENARIOS[case][1]
        events = [{"Package": PACKAGE, "Test": case, "Action": action} for action in ("run", "pass")]
        if case == RESUME:
            report["launches"][1].update(launch_id="old", image_sha256="a"*64)
            report["launches"].append(dict(pid=5, launch_id="new", image_sha256="a"*64, kernel_exit=True, reaped=True, endpoint_absent=True))
            obs.update(old_controller_pid=2, new_controller_pid=5, resume_proof_rotated=True)
            obs["mcp_processes"] += [dict(pid=6, reaped=True, cleanup_confirmed=True), dict(pid=7, reaped=True, cleanup_confirmed=True)]
        else:
            obs.update(management_action="code.revoke" if case == REVOKE else "code.rotate", mcp_progress_seen=True,
                       terminal_status="authorization_lost", transport_after_terminal=0,
                       task_processes={role: dict(role=role, pid=pid, parent_pid=parent, image_bound=True, initially_live=True,
                                                 exited_at_terminal=True, exited_at_cleanup=True)
                                       for role, pid, parent in (("root", 5, 1), ("leaf", 6, 5))})
        return events, report

    # A preserved PID or launch cannot masquerade as real Runtime replacement.
    def test_rejects_resume_without_restart_or_rotated_proof(self):
        events, report = self.extended_report(RESUME)
        validate(events, report, RESUME)
        for mutate in (lambda r: r["launches"][2].update(launch_id="old"),
                       lambda r: r["observations"].update(resume_proof_rotated=False)):
            bad = copy.deepcopy(report)
            mutate(bad)
            with self.assertRaises(ValueError):
                validate(events, bad, RESUME)

    # Live/exit observations are essential, not just an authorization error text.
    def test_rejects_management_without_independent_task_exit(self):
        for case in (REVOKE, ROTATE):
            events, report = self.extended_report(case)
            validate(events, report, case)
            report["observations"]["task_processes"]["leaf"]["exited_at_terminal"] = False
            with self.assertRaises(ValueError):
                validate(events, report, case)

    # A historical stopped PID may be reused; live owners/heads must still differ.
    def test_allows_historical_pid_reuse_but_rejects_live_collision(self):
        events, report = self.extended_report(RESUME)
        report["observations"]["mcp_processes"][-1]["pid"] = 2
        validate(events, report, RESUME)
        report["observations"]["mcp_processes"][-1]["pid"] = 5
        with self.assertRaises(ValueError):
            validate(events, report, RESUME)


if __name__ == "__main__":
    unittest.main()
