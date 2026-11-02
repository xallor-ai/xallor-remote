import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const root=process.env.XALLOR_TEST_CLUSTER, device=process.env.XALLOR_TEST_TARGET, executable=process.env.XALLOR_TEST_PROGRAM;
assert(root && fs.readFileSync(path.join(root,".owner"),"utf8")==="runtime-cluster");
const ipc=new RuntimeIpc();
await ipc.connect();
const exec=async input=>{
  let stdout="";
  const frame=await ipc.stream("exec",{device_id:device,...input},f=>{if(f.event==="stdout")stdout+=f.data||"";});
  return {frame,stdout};
};
try {
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
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(root,"target/jobs.json"),"utf8")).jobs,{});
  console.log("PASS final execution contract, native argv, target errors, no leaked jobs");
} finally {ipc.close();}
