"""Bounded installed-head protocol observations must not hide failure as success."""
from pathlib import Path
import sys
import tempfile
import unittest

from fixtures.package_stdio import StdioHead


class PackageStdioTests(unittest.TestCase):
    def start(self,script):
        temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup)
        root=Path(temp.name);source=root/'head.py';source.write_text(script,encoding='utf-8')
        return StdioHead([sys.executable,'-u',str(source)],None,root/'stderr.log')

    # A real child responds on stdio and is reaped during cleanup.
    def test_accepts_correlated_reply_and_observes_child_exit(self):
        head=self.start('import sys,json\nfor line in sys.stdin:\n m=json.loads(line)\n if "id" in m: print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"ok":True}}),flush=True)\n')
        try:self.assertEqual(head.call('probe',{},2),{'ok':True})
        finally:result=head.close()
        self.assertTrue(result['reaped']);self.assertFalse(head.reader.is_alive())

    # Invalid or oversized output must produce a failure without indefinite reads.
    def test_rejects_malformed_and_oversized_reply(self):
        for output in ['not-json','x'*1048577]:
            head=self.start('import sys\nsys.stdin.readline()\nprint('+repr(output)+',flush=True)\n')
            try:
                with self.assertRaises(RuntimeError):head.call('probe',{},2)
            finally:head.close()

    # An error envelope is not a normal successful tool result.
    def test_rejects_protocol_error(self):
        head=self.start('import sys,json\nm=json.loads(sys.stdin.readline())\nprint(json.dumps({"id":m["id"],"error":{"code":-1}}),flush=True)\n')
        try:
            with self.assertRaises(RuntimeError):head.call('probe',{},2)
        finally:head.close()


if __name__=='__main__':unittest.main()
