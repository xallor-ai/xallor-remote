import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const root = process.env.XALLOR_TEST_CLUSTER;
assert(root && fs.readFileSync(path.join(root, ".owner"), "utf8") === "runtime-cluster");
const target = process.env.XALLOR_TEST_TARGET;
const read = name => JSON.parse(fs.readFileSync(path.join(root, `${name}.json`), "utf8"));
const alice = read("alice"), bob = read("bob");
const run = promisify(execFile);
const cli = (profile, ...args) => run(process.env.XALLOR_TEST_CLI, args, { env: { ...process.env,
  XALLOR_REMOTE_DATA_DIR: path.join(root, profile) }, windowsHide: true, timeout: 20000 });
const account = (profile, name) => cli(profile, "account", "use", "--credential-file", path.join(root, `${name}.json`));
const manage = async (...args) => JSON.parse((await cli("target", "device", ...args)).stdout);
const client = new RuntimeIpc();
await client.connect();
const call = (method, parameters = {}) => client.call(method, { device_id: target, ...parameters });
const results = [];
const pass = name => { results.push({ name, status: "pass" }); console.log(name); };
// Ownership changes close old channels. Only this read-only info check retries
// a transient channel failure; commands and management mutations are never replayed.
async function info(expected) {
  for (let n = 0; n < 20; n++) {
    const result = await call("info");
    if (result.code === "relay_error") { await new Promise(r => setTimeout(r, 50)); continue; }
    assert.equal(expected === "ok" ? result.ok : result.code, expected === "ok" ? true : expected);
    return result;
  }
  throw Error("info did not settle after ownership change");
}
try {
  await info("ok");
  assert.equal((await call("write", { path: "shared.txt", content: "allowed-read" })).ok, true);
  const first = await manage("claim");
  assert.equal(first.owner_tenant, alice.tenant_id);
  assert.equal(first.owner_principal, alice.principal_id);
  await info("unauthorized");
  pass("claim_revokes_existing_bearer_and_other_tenant_connection");
  await account("controller", "alice");
  await cli("controller", "peer", "add", "--device", target);
  await info("ok");
  const share = await manage("share", "--member", bob.membership_id, "--allow", "info,read", "--ttl", "1h");
  await account("controller", "bob");
  await info("ok");
  assert.equal((await call("read", { path: "shared.txt" })).result.content, "allowed-read");
  assert.equal((await call("write", { path: "forbidden.txt", content: "no" })).code, "unauthorized");
  assert.equal((await client.stream("exec", { device_id: target, script: { text: "echo forbidden" } }, () => {})).code, "unauthorized");
  assert.equal((await call("handoff.submit", { request: {} })).code, "unauthorized");
  assert(!fs.existsSync(path.join(root, "workspaces/target/forbidden.txt")));
  pass("cross_tenant_share_allows_only_explicit_operations");
  await account("controller", "colleague");
  await info("unauthorized");
  pass("same_tenant_membership_does_not_grant_device_access");
  await account("controller", "bob");
  await info("ok");
  await manage("unshare", "--share", share.shares[0].id);
  await info("unauthorized");
  const replaced = await manage("share", "--member", bob.membership_id, "--allow", "info,read", "--ttl", "1h");
  assert.notEqual(replaced.shares[0].id, share.shares[0].id);
  await info("unauthorized");
  await account("controller", "bob");
  await info("ok");
  pass("regrant_requires_fresh_binding_after_revocation");
  await account("controller", "alice");
  await info("ok");
  const transferred = await manage("transfer", "--member", bob.membership_id);
  assert.equal(transferred.owner_tenant, bob.tenant_id);
  assert.equal(transferred.shares.length, 0);
  assert.notEqual(transferred.ownership_id, first.ownership_id);
  await info("unauthorized");
  await assert.rejects(cli("target", "device", "owner"));
  await account("target", "bob");
  await account("controller", "bob");
  await info("ok");
  pass("transfer_revokes_owner_and_removes_prior_shares");
  await cli("controller", "account", "clear");
  const shown = await cli("target", "grant", "show");
  const grant = shown.stdout.match(/授权码:\s*(\S+)/)?.[1];
  assert(grant, "target grant show did not return an authorization code");
  await cli("controller", "peer", "add", "--device", target, "--grant", grant);
  await info("unauthorized");
  pass("clearing_account_cannot_bypass_claimed_device_access");
  await account("controller", "bob");
  await info("ok");
  const back = await manage("transfer", "--member", alice.membership_id);
  assert.notEqual(back.ownership_id, first.ownership_id);
  await info("unauthorized");
  await account("target", "alice");
  await account("controller", "alice");
  await cli("controller", "peer", "add", "--device", target);
  await info("ok");
  pass("transfer_back_creates_a_new_ownership_instance");
  fs.writeFileSync(path.join(root, "ownership-results.json"), JSON.stringify(results));
} finally { client.close(); }
