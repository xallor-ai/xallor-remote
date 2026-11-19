"""Bounded stdio client retaining the actual installed Node child until exit."""
import json
import os
import queue
import subprocess
import threading
import time


class StdioHead:
    def __init__(self,command,env,stderr):
        self.log=stderr.open('wb');self.process=None;self.error=None;self.seq=0;self.uncertain=False
        self.messages=queue.Queue(maxsize=32)
        quiet={'creationflags':subprocess.CREATE_NO_WINDOW} if os.name=='nt' else {}
        try:
            self.process=subprocess.Popen(command,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.log,**quiet)
        except BaseException:
            self.log.close();raise
        self.reader=threading.Thread(target=self.collect,daemon=True);self.reader.start()

    def collect(self):
        try:
            while True:
                line=self.process.stdout.readline(1048577)
                if not line:
                    self.messages.put_nowait(None);return
                if len(line)>1048576:raise ValueError('MCP response exceeds observation budget')
                value=json.loads(line)
                if not isinstance(value,dict):raise ValueError('Invalid MCP message')
                self.messages.put_nowait(value)
        except Exception as error:
            self.error=error

    def send(self,method,params,request_id=None):
        message=dict(jsonrpc='2.0',method=method,params=params)
        if request_id is not None:message['id']=request_id
        self.process.stdin.write(json.dumps(message).encode()+b'\n');self.process.stdin.flush()

    def call(self,method,params,timeout=240):
        self.seq+=1;request_id=self.seq;self.send(method,params,request_id)
        deadline=time.monotonic()+timeout
        for _ in range(128):
            while True:
                if self.error is not None:raise RuntimeError('Invalid MCP response stream') from self.error
                remaining=deadline-time.monotonic()
                if remaining<=0:
                    self.uncertain=True;raise TimeoutError('MCP observation timed out; operation not replayed')
                try:message=self.messages.get(timeout=min(.1,remaining));break
                except queue.Empty:continue
            if message is None:raise RuntimeError('MCP head exited before matching response')
            if message.get('id')!=request_id:continue
            if 'error' in message or 'result' not in message:raise RuntimeError('MCP returned a protocol error')
            return message['result']
        raise RuntimeError('MCP notification budget exceeded')

    def close(self):
        forced=False
        try:
            try:self.process.stdin.close()
            except BrokenPipeError:pass
            try:self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                forced=True;self.process.terminate();self.process.wait(timeout=10)
            self.reader.join(timeout=3)
            if self.reader.is_alive():raise RuntimeError('MCP output reader remains live after head exit')
            self.process.stdout.close()
            return dict(pid=self.process.pid,exit_code=self.process.returncode,reaped=True,terminated_for_cleanup=forced)
        finally:
            self.log.close()
