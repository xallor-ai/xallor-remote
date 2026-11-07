import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const root=process.env.XALLOR_TEST_CLUSTER, device=process.env.XALLOR_TEST_TARGET;
assert(root && fs.readFileSync(path.join(root,".owner"),"utf8")==="runtime-cluster");
const ipc=new RuntimeIpc(), streams=[];
const pause=ms=>new Promise(r=>setTimeout(r,ms));
const jobs=()=>JSON.parse(fs.readFileSync(path.join(root,"target/jobs.json"),"utf8")).jobs;
await ipc.connect();
try {
  for(let n=0;n<4;n++) {
    const connection=new RuntimeIpc(); await connection.connect();
    const task={connection,id:"",observed:false,done:false}; streams.push(task);
    task.result=connection.stream("exec",{device_id:device,program:{executable:process.env.XALLOR_TEST_PROGRAM,args:["wait"]},timeout_ms:45000},event=>{
      if(event.exec_id)task.id=event.exec_id;
      if(event.data?.includes("result-observed-output"))task.observed=true;
    }).then(frame=>{task.done=true;return frame;});
    task.result.catch(()=>{});
    const deadline=Date.now()+15000;
    while(!task.observed) { assert(!task.done,"fixture ended before output");assert(Date.now()<deadline,"fixture did not start");await pause(25); }
  }
  assert.equal(Object.keys(jobs()).length,4);
  assert(streams.every(task=>!task.done));
  const info=await ipc.call("info",{device_id:device});
  assert.equal(info.ok,true,"full jobs blocked info: "+JSON.stringify(info));
  assert(JSON.parse(info.result.content).os);
  assert.equal((await ipc.call("write",{device_id:device,path:"capacity.txt",content:"still reachable"})).ok,true);
  const read=await ipc.call("read",{device_id:device,path:"capacity.txt"});
  assert.equal(read.ok,true);assert.equal(read.result.content,"still reachable");
  let output="";
  const denied=await ipc.stream("exec",{device_id:device,script:{text:"echo must-not-run"}},e=>{output+=e.data||"";});
  assert.equal(denied.code,"quota_exceeded");assert.equal(output,"");assert.equal(Object.keys(jobs()).length,4);
  assert(streams.every(task=>!task.done),"live jobs were disturbed by observation or rejection");
  for(const task of streams) {
    assert.equal((await task.connection.call("exec.cancel",{exec_id:task.id})).ok,true);
    assert.equal((await task.result).result.status,"cancelled");
  }
  assert.equal(Object.keys(jobs()).length,0);
  const next=await ipc.stream("exec",{device_id:device,script:{text:"echo capacity-restored"}},()=>{});
  assert.equal(next.result.exit_code,0);
  console.log("PASS independent transport/job limits, observation while full, cancellation and reuse");
} finally {
  for(const task of streams)task.connection.close();
  ipc.close();
}
