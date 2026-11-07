import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const root=process.env.XALLOR_TEST_CLUSTER, device=process.env.XALLOR_TEST_TARGET, executable=process.env.XALLOR_TEST_PROGRAM;
assert(root && fs.readFileSync(path.join(root,".owner"),"utf8")==="runtime-cluster");
const ipc=new RuntimeIpc();
await ipc.connect();
const target=new RuntimeIpc();
const exec=async input=>{
  let stdout="";
  const frame=await ipc.stream("exec",{device_id:device,...input},f=>{if(f.event==="stdout")stdout+=f.data||"";});
  return {frame,stdout};
};
try {
  const profile=process.env.XALLOR_REMOTE_DATA_DIR;
  process.env.XALLOR_REMOTE_DATA_DIR=path.join(root,"target");
  try { await target.connect(); }
  finally { process.env.XALLOR_REMOTE_DATA_DIR=profile; }
  const info=await ipc.call("info",{device_id:device});
  assert.deepEqual(JSON.parse(info.result.content).execution_modes,["script","program"]);
  for(const input of [{command:"echo obsolete"},{},{script:{text:"echo both"},program:{executable}},
    {script:{text:"x",unknown:true}},{program:{executable,args:["\0"]}},
    {program:{executable,args:["\ud800"]}},{program:{executable,args:[null]}},
    {script:{text:"x"},timeout_ms:-1}]) {
    const {frame,stdout}=await exec(input);
    assert.equal(frame.code,"invalid_execution",JSON.stringify(frame)); assert.equal(stdout,"");
  }
  const {frame:missing}=await exec({program:{executable:path.join(root,"not-installed")}});
  assert.equal(missing.code,"program_unavailable");
  const {frame:wrongShell}=await exec({script:{text:"echo no",shell:process.platform==="win32"?"bash":"powershell"}});
  assert.equal(wrongShell.code,"unsupported_shell");
  const args=["", "a b", '$HOME', '`', "中文🌍", 'a"b'];
  const {frame,stdout}=await exec({program:{executable,args}});
  assert.equal(frame.result.exit_code,0); assert.deepEqual(JSON.parse(stdout).args,args);
  const {frame:nonzero}=await exec({program:{executable,args:["exit-seven"]}});
  assert.equal(nonzero.result.exit_code,7);
  if(process.platform==="win32") {
    const {frame:batch}=await exec({program:{executable:"cmd.exe",args:["/c","echo no"]}});
    assert.equal(batch.code,"unsupported_program");
    const {frame:large}=await exec({program:{executable,args:['"'.repeat(20000)]}});
    assert.equal(large.code,"too_large");
  }
  // Configure the real Runtime through IPC. Its inherited working directory
  // matches this fixture's, so a relative workspace has an unambiguous anchor.
  const workspace=path.join(root,"workspaces/target");
  const program=path.join(workspace,path.basename(executable));
  fs.copyFileSync(executable,program); fs.chmodSync(program,0o700);
  const before=await target.call("config.get"); assert.equal(before.ok,true);
  try {
    for(const configured of [workspace,path.relative(process.cwd(),workspace)]) {
      assert.equal((await target.call("config.set",{workspace:configured})).ok,true);
      for(const cwd of [undefined,".",workspace]) {
        for(const binary of [program,"./"+path.basename(program)]) {
          const {frame,stdout}=await exec({program:{executable:binary,args:["relative-ready"]},...(cwd===undefined?{}:{cwd})});
          assert.equal(frame.ok,true,JSON.stringify(frame)); assert.equal(frame.result.exit_code,0);
          const data=JSON.parse(stdout); assert.deepEqual(data.args,["relative-ready"]);
          assert.equal(fs.realpathSync(data.cwd),fs.realpathSync(workspace));
        }
        const script=process.platform==="win32"?"[Console]::WriteLine((Get-Location).Path)":"pwd";
        const {frame,stdout}=await exec({script:{text:script},...(cwd===undefined?{}:{cwd})});
        assert.equal(frame.ok,true); assert.equal(frame.result.exit_code,0);
        assert.equal(fs.realpathSync(stdout.trim()),fs.realpathSync(workspace));
      }
      const escaped=await exec({program:{executable:program},cwd:path.dirname(workspace)});
      assert.equal(escaped.frame.code,"policy_deny"); assert.equal(escaped.stdout,"");
    }
  } finally { assert.equal((await target.call("config.set",before.result)).ok,true); }
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(root,"target/jobs.json"),"utf8")).jobs,{});
  console.log("PASS execution contract, relative/absolute paths, scripts, workspace boundary, no leaked jobs");
} finally {target.close();ipc.close();}
