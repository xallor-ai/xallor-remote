"""Reviewed metadata table inventory for cross-layer Relay acceptance.

This is a schema guard, not a proof of every stored value's privacy properties.
Operation-payload and credential assertions remain in their scenario tests.
"""

VERSION = 13
TABLES = frozenset(("authority", "device_grants", "device_ownership", "device_shares",
                    "devices", "execution_signing_key", "memberships", "oidc_issuers",
                    "oidc_subject_mappings", "principal_credentials", "principals", "tenants",
                    "access_codes", "code_consumptions", "code_rotations", "resume_capabilities",
                    "session_authorizations", "session_targets"))


def assert_relay_metadata_schema(db):
    version = db.execute("PRAGMA user_version").fetchone()[0]
    tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    if version != VERSION:
        raise AssertionError(f"Unexpected Relay schema version: {version}; expected {VERSION}")
    if tables != TABLES:
        raise AssertionError(f"Relay table inventory differs: missing={sorted(TABLES-tables)}, unexpected={sorted(tables-TABLES)}")
