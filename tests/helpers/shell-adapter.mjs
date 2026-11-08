import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const root=process.env.XALLOR_TEST_CLUSTER, device=process.env.XALLOR_TEST_TARGET;
assert(root && fs.readFileSync(path.join(root,".owner"),"utf8")==="runtime-cluster");
const target=new RuntimeIpc(), controller=new RuntimeIpc();
const results=[];
function ok(frame){ assert.equal(frame.ok,true,JSON.stringify(frame)); return frame.result; }
async function bounded(promise){
  let timer;
  try{return await Promise.race([promise,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error("shell test deadline")),20000);})]);}
  finally{clearTimeout(timer);}
}
try {
  process.env.XALLOR_REMOTE_DATA_DIR=path.join(root,"target"); await target.connect();
  process.env.XALLOR_REMOTE_DATA_DIR=path.join(root,"controller"); await controller.connect();
  const info=async()=>JSON.parse(ok(await bounded(controller.call("info",{device_id:device}))).content);
  const initial=await info();
  assert.equal(initial.shell.selected,process.env.XALLOR_TEST_SHELL);
  assert.equal(initial.shell.configured,process.env.XALLOR_TEST_SHELL);
  assert.equal(initial.execution_backend.name,"native_user");
  assert.equal(initial.execution_backend.available,true);
  assert.equal(initial.execution_backend.privilege,"same_user");
  assert.equal(initial.execution_backend.sandbox,"none");
  assert.equal(initial.execution_backend.process_tree,initial.os === "windows" ? "job_object" : "process_group");
  const literal="中文 🌍 $HOME ` \"double\" 'single'";
  const filename="space ' 中文.txt";
  for(const name of initial.shell.available){
    ok(await bounded(target.call("config.set",{shell:name})));
    assert.equal((await info()).shell.selected,name);
    const powershell=name==="powershell"||name==="pwsh";
    const quote=s=>powershell?"'"+s.replaceAll("'","''")+"'":"'"+s.replaceAll("'","'\"'\"'")+"'";
    const script=powershell
      ? `[IO.File]::WriteAllText(${quote(path.join(root,"workspaces/target",filename))}, ${quote(literal)});\nWrite-Output ${quote(literal)}\nexit 7`
      : `printf '%s' ${quote(literal)} > ${quote(filename)}\nprintf '%s\\n' ${quote(literal)}\nexit 7`;
    let output="";
    const result=ok(await bounded(controller.stream("exec",{device_id:device,script:{text:script}},f=>{output+=f.data||"";})));
    assert.equal(result.exit_code,7); assert.equal(result.status,"completed");
    assert(output.includes(literal),output);
    assert.equal(ok(await bounded(controller.call("read",{device_id:device,path:filename}))).content,literal);
    let errors="";
    const failed=ok(await bounded(controller.stream("exec",{device_id:device,script:{text:powershell?"Write-Error '中文 error'":"printf '中文 error' >&2; exit 1"}},f=>{if(f.event==="stderr")errors+=f.data||"";})));
    assert.equal(failed.exit_code,1); assert(errors.includes("中文 error"),errors); assert(!errors.includes("CLIXML"),errors);
    if (powershell) {
      const declarationScripts = [
        "using namespace System.Text\n[StringBuilder]::new('declaration-ready 中文🌍').ToString()",
        "param([string]$Label = 'declaration-ready 中文🌍')\nWrite-Output $Label",
        "using namespace System.Text\n[CmdletBinding()]\nparam([string]$Label = 'declaration-ready 中文🌍')\n[StringBuilder]::new($Label).ToString()",
      ];
      for (const text of declarationScripts) {
        let stdout = "", stderr = "";
        const declared = ok(await bounded(controller.stream("exec", {device_id:device,script:{text,shell:name}}, f => {
          if (f.event === "stdout") stdout += f.data || "";
          if (f.event === "stderr") stderr += f.data || "";
        })));
        assert.equal(declared.exit_code,0); assert.equal(stdout.trim(),"declaration-ready 中文🌍"); assert.equal(stderr,"");
      }
      let namedStdout="", namedStderr="";
      const named=ok(await bounded(controller.stream("exec",{device_id:device,script:{text:"begin { Write-Output 'named-block-ready' } end { Write-Output 'named-block-done' }",shell:name}},f=>{
        if (f.event==="stdout") namedStdout+=f.data||"";
        if (f.event==="stderr") namedStderr+=f.data||"";
      })));
      assert.equal(named.exit_code,0);
      assert.equal(namedStdout.replace(/\r\n/g,"\n").trim(),"named-block-ready\nnamed-block-done");
      assert.equal(namedStderr,"");
      let failureOut="", failureError="";
      const failure=ok(await bounded(controller.stream("exec",{device_id:device,script:{
        text:"begin { Write-Output 'before-error' } end { Write-Error 'named-block-failure' }",shell:name}},f=>{
        if(f.event==="stdout") failureOut+=f.data||"";
        if(f.event==="stderr") failureError+=f.data||"";
      })));
      assert.equal(failure.exit_code,1); assert.equal(failure.status,"completed");
      assert.equal(failureOut.trim(),"before-error"); assert(failureError.includes("named-block-failure"));
      results.push({name:name+"_leading_declarations",status:"pass"});
      results.push({name:name+"_named_blocks_and_failure_status",status:"pass"});
    }
    results.push({name,status:"pass"});
  }
  const before=ok(await target.call("config.get"));
  assert.equal((await bounded(target.call("config.set",{shell:"unknown --command",workspace:"must-not-create"}))).ok,false);
  assert.deepEqual(ok(await target.call("config.get")),before);
  assert.equal(fs.existsSync(path.join(root,"must-not-create")),false);
  if (!initial.shell.available.includes("pwsh")) {
    ok(await bounded(target.call("config.set",{shell:"pwsh"})));
    const unavailable=await info();
    assert.equal(unavailable.shell.code,"shell_unavailable");
    assert.equal(unavailable.shell.selected,undefined);
    const denied=await bounded(controller.stream("exec",{device_id:device,script:{text:"echo must-not-run"}},f=>{
      if ((f.event==="stdout"||f.event==="stderr") && (f.data||"")!=="") throw Error("missing shell produced output");
    }));
    assert.equal(denied.code,"shell_unavailable");
    results.push({name:"explicit_missing_interpreter_does_not_fallback",status:"pass"});
  }
  ok(await bounded(target.call("config.set",{shell:process.env.XALLOR_TEST_SHELL})));
  const dest=path.resolve("dist/verification/shell-adapter.json");
  fs.mkdirSync(path.dirname(dest),{recursive:true});fs.writeFileSync(dest,JSON.stringify(results,null,2));
  console.log(JSON.stringify(results));
} finally {target.close();controller.close();}
