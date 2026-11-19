package runtimeowner

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const maxReconcileRevision int64 = 1_000_000_000

// ReconcileObservation accepts a caller's retained CAS coordinates and only
// returns the same operation observation as runtime.operation.status. It never
// appends a resolution or invokes an execution lifecycle.
func (j Journal) ReconcileObservation(req ipc.Request) ipc.Frame {
	var p ReconcileRequest
	d := json.NewDecoder(bytes.NewReader(req.Params))
	d.DisallowUnknownFields()
	if req.Method != "runtime.operation.reconcile" || len(req.Params) > 512 || d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !validReconcileObservationRequest(p) {
		return ipc.Fail(req.ID, protocol.AgentError, "需要有效的本机事务观察参数。")
	}
	namespace, err := j.NamespaceID()
	if err != nil || p.NamespaceSHA256 != namespace {
		return ipc.Fail(req.ID, protocol.RuntimeProfileMismatch, "事务记录不属于当前 Runtime profile。")
	}
	beforeRevision, beforeDigest, err := j.observedRevisionDigest(p.OperationID)
	if errors.Is(err, ErrUnknown) {
		return ipc.Fail(req.ID, protocol.RuntimeOperationUnknown, "未找到该升级事务的记录；结果尚未确认，请勿重放请求。")
	}
	if err != nil {
		return ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "无法读取本机升级事务，请保留现有记录并检查运行状态。")
	}
	if beforeRevision != p.ExpectedRevision || beforeDigest != p.ExpectedDigest {
		return ipc.Fail(req.ID, protocol.AgentError, "事务记录版本或摘要已变化，请重新读取后再观察。")
	}
	status := j.QueryOperation(ipc.Request{ID: req.ID, Method: "runtime.operation.status", Params: mustJSON(map[string]string{"operation_id": p.OperationID})})
	if status.OK == nil || !*status.OK {
		return status
	}
	var meta struct {
		Operation json.RawMessage `json:"operation"`
		Settled   bool            `json:"settled"`
		Observer  json.RawMessage `json:"observer"`
	}
	if json.Unmarshal(status.Result, &meta) != nil {
		return ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "无法读取本机升级事务，请保留现有记录并检查运行状态。")
	}
	revision, digest, err := j.observedRevisionDigest(p.OperationID)
	if errors.Is(err, ErrUnknown) {
		return ipc.Fail(req.ID, protocol.RuntimeOperationUnknown, "未找到该升级事务的记录；结果尚未确认，请勿重放请求。")
	}
	if err != nil {
		return ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "无法读取本机升级事务，请保留现有记录并检查运行状态。")
	}
	if revision != beforeRevision || digest != beforeDigest || revision != p.ExpectedRevision || digest != p.ExpectedDigest {
		return ipc.Fail(req.ID, protocol.AgentError, "事务记录版本或摘要已变化，请重新读取后再观察。")
	}
	result := map[string]any{}
	if err := json.Unmarshal(status.Result, &result); err != nil {
		return ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "无法读取本机升级事务，请保留现有记录并检查运行状态。")
	}
	result["revision"] = revision
	result["original_record_sha256"] = digest
	result["disposition"] = DispositionUnresolved
	result["blockers"] = []Blocker{{Code: "native_owner_observation_required"}}
	return ipc.OK(req.ID, result)
}

func validReconcileObservationRequest(p ReconcileRequest) bool {
	return p.valid() && shaPattern.MatchString(p.ExpectedDigest) && p.ExpectedRevision <= maxReconcileRevision
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func (j Journal) observedRevisionDigest(id string) (int64, string, error) {
	db, err := j.open(false)
	if os.IsNotExist(err) {
		return 0, "", ErrUnknown
	}
	if err != nil {
		return 0, "", err
	}
	defer db.Close()
	var raw, resolution []byte
	var revision, active int64
	err = db.QueryRow("SELECT record,resolution,revision,active FROM operations WHERE id=?", id).Scan(&raw, &resolution, &revision, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrUnknown
	}
	if err != nil || len(raw) > maxRecordBytes || len(resolution) > maxRecordBytes || revision < 1 || (active != 0 && active != 1) {
		return 0, "", ErrInvalid
	}
	return revision, digestRecord(raw), nil
}

// QueryOperation is shared by the serving owner and the transaction endpoint.
// It exposes no bundle path and never executes, resumes or reconciles an action.
func (j Journal) QueryOperation(req ipc.Request) ipc.Frame {
	var p struct {
		ID string `json:"operation_id"`
	}
	d := json.NewDecoder(bytes.NewReader(req.Params))
	d.DisallowUnknownFields()
	if req.Method != "runtime.operation.status" || len(req.Params) > 256 || d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !idPattern.MatchString(p.ID) {
		return ipc.Fail(req.ID, protocol.AgentError, "需要有效的本机升级事务 ID。")
	}
	r, settled, err := j.QuerySettled(p.ID)
	if errors.Is(err, ErrUnknown) {
		return ipc.Fail(req.ID, protocol.RuntimeOperationUnknown, "未找到该升级事务的记录；结果尚未确认，请勿重放请求。")
	}
	if err != nil {
		return ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "无法读取本机升级事务，请保留现有记录并检查运行状态。")
	}
	return ipc.OK(req.ID, map[string]any{"operation": r, "settled": settled, "observer": buildinfo.Running()})
}
