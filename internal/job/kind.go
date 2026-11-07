package job

// Kind identifies the lifecycle that owns a durable local resource slot. The
// public reservation methods intentionally expose these lifecycles separately;
// transport request types must not choose a persisted resource category.
type Kind string

const (
	KindExec    Kind = "exec"
	KindHandoff Kind = "handoff"
)

func (k Kind) valid() bool { return k == KindExec || k == KindHandoff }
