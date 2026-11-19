"""Reject narrative/nonzero-exit substitutes for same-operation restoration."""
from copy import deepcopy
import hashlib
import json
import unittest

from fixtures.restore_evidence import candidate_selection, identity, live_binding, restored_operation


class RestoreEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.a={"build":{"version":"0.2.7-rc.1"},"artifact":{"sha256":"a"*64}}
        self.b={"build":{"version":"0.2.7-rc.2"},"artifact":{"sha256":"b"*64}}
        self.raw=json.dumps(self.b).encode()
        self.before={"current":self.a,"current_activation":"a"*32,"current_launch_id":"b"*32}
        self.pending={**self.before,"pending":self.b,"pending_activation":"c"*32,"pending_launch_id":"d"*32}
        old=identity(self.before,"current");target=identity(self.pending,"pending")
        original={"state_sha256":"1"*64,"current":old,"previous":{},"pending":{}}
        waiting={"state_sha256":"2"*64,"current":old,"previous":{},"pending":target}
        restored={**old,"launch_id":"d"*32}
        final={"state_sha256":"3"*64,"current":restored,"previous":{},"pending":{}}
        self.binding={"baseline":original,"requested":{"sha256":"b"*64,"activation_id":"c"*32},"manifest":self.b,
                      "manifest_sha256":hashlib.sha256(self.raw).hexdigest(),"clear_only":False}
        self.admitted={"request":{"id":"e"*32,"kind":"install","intent_sha256":"f"*64},"namespace_sha256":"9"*64,"outcome":"pending","binding":self.binding}
        def effect(purpose,name,before,after,target,state="confirmed"):
            return {"purpose":purpose,"name":name,"state":state,"before":before,"observed":after,"target":target}
        effects=[effect("requested","prepare",original,waiting,target),effect("requested","stop",waiting,waiting,{}),
                 effect("requested","start",waiting,waiting,target,"quiescent_failure"),
                 effect("restore","start",waiting,waiting,restored),effect("restore","commit",waiting,final,restored),
                 effect("restore","ready",final,final,restored)]
        effects[1].update(stop={"peer_pid":123,"selection":old},stop_observed={"process":"exit_observed"})
        self.final={"source":"journal_read_only","settled":True,"operation":{**self.admitted,"outcome":"failed","effects":effects}}
        self.target=target

    # Full correlated B-failure/A-restoration evidence returns the new A target.
    def test_accepts_exact_failed_start_and_confirmed_restore_sequence(self):
        result=restored_operation(self.final,self.admitted,self.binding,self.target,123)
        self.assertEqual(result["sha256"],"a"*64)
        self.assertEqual(result["launch_id"],"d"*32)

    # Neither another receipt nor unsettled work can prove this request recovered.
    def test_rejects_foreign_identity_or_unsettled_receipt(self):
        for mutate in (lambda r:r.update(settled=False),lambda r:r["operation"].update(outcome="uncertain"),
                       lambda r:r["operation"]["request"].update(id="0"*32),lambda r:r["operation"].update(namespace_sha256="0"*64)):
            bad=deepcopy(self.final);mutate(bad)
            with self.assertRaises(ValueError): restored_operation(bad,self.admitted,self.binding,self.target,123)

    # A cancellation/refusal, missing ready, wrong stopped PID or broken state
    # chain cannot be relabeled as successful failure recovery.
    def test_rejects_missing_or_unconfirmed_effects(self):
        for mutate in (lambda e:e[2].update(state="not_applied"),lambda e:e.pop(),
                       lambda e:e[1]["stop"].update(peer_pid=124),lambda e:e[3].update(before={}),
                       lambda e:e[4].update(state="unknown")):
            bad=deepcopy(self.final);mutate(bad["operation"]["effects"])
            with self.assertRaises(ValueError): restored_operation(bad,self.admitted,self.binding,self.target,123)

    # Recognition requires the active transaction and exact requested raw bytes.
    def test_live_binding_rejects_history_or_different_manifest_bytes(self):
        live={"source":"lifecycle_ipc","settled":False,"operation":self.admitted}
        self.assertEqual(live_binding(live,self.before,self.b,self.raw),self.binding)
        with self.assertRaises(ValueError): live_binding(live,self.before,self.b,self.raw+b" ")
        bad=deepcopy(live);bad["operation"]["outcome"]="failed"
        with self.assertRaises(ValueError): live_binding(bad,self.before,self.b,self.raw)

    # Pending selection must still be the original baseline and admitted B.
    def test_candidate_selection_rejects_unrelated_launch_or_activation(self):
        record={**self.admitted,"effects":deepcopy(self.final["operation"]["effects"][:2])}
        self.assertEqual(candidate_selection(self.binding,record),self.target)
        for field in ("activation_id","sha256"):
            bad=deepcopy(record);bad["effects"][-1]["observed"]["pending"][field]="f"*32
            with self.assertRaises(ValueError):candidate_selection(self.binding,bad)

    # Preparing an intended B is not proof it was written. Only Before or a
    # completed Observed selection may be used; unknown effects are refused.
    def test_candidate_selection_never_promotes_expected_intent(self):
        record=deepcopy(self.admitted)
        prepare=deepcopy(self.final["operation"]["effects"][0])
        prepare.update(state="pending",expected=prepare.pop("observed"))
        record["effects"]=[prepare]
        self.assertIsNone(candidate_selection(self.binding,record))
        prepare.update(state="unknown")
        with self.assertRaises(ValueError):candidate_selection(self.binding,record)


if __name__=="__main__": unittest.main()
