"""Keep metadata acceptance current without allowing new payload collections."""
import sqlite3
import unittest
from fixtures.relay_schema_contract import assert_relay_metadata_schema

CURRENT_TABLES = (
    "access_codes", "authority", "code_consumptions", "code_rotations",
    "device_grants", "device_ownership", "device_shares", "devices",
    "execution_signing_key", "memberships", "oidc_issuers", "oidc_subject_mappings",
    "principal_credentials", "principals", "resume_capabilities",
    "session_authorizations", "session_targets", "tenants",
)


class RelaySchemaContractTests(unittest.TestCase):
    def setUp(self):
        self.db = sqlite3.connect(":memory:")
        self.addCleanup(self.db.close)
        for name in CURRENT_TABLES:
            self.db.execute(f'CREATE TABLE "{name}" (id TEXT)')
        self.db.execute("PRAGMA user_version=13")

    # Current auth/session metadata is legitimate; it is not an execution log.
    def test_accepts_current_reviewed_auth_metadata_inventory(self):
        assert_relay_metadata_schema(self.db)

    # Merely bumping a version must not permit an operation payload collection.
    def test_rejects_unexpected_payload_collection(self):
        self.db.execute("CREATE TABLE command_outputs (payload TEXT)")
        with self.assertRaisesRegex(AssertionError, "unexpected=.*command_outputs"):
            assert_relay_metadata_schema(self.db)

    # A missing identity/session table is not a valid reduced schema.
    def test_rejects_missing_session_metadata(self):
        self.db.execute("DROP TABLE session_authorizations")
        with self.assertRaisesRegex(AssertionError, "missing=.*session_authorizations"):
            assert_relay_metadata_schema(self.db)

    # Unknown future schemas need review instead of implicit acceptance.
    def test_rejects_old_and_future_versions(self):
        for version in (8, 14):
            self.db.execute(f"PRAGMA user_version={version}")
            with self.assertRaisesRegex(AssertionError, "version"):
                assert_relay_metadata_schema(self.db)
