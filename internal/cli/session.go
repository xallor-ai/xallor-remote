package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type sessionRequestOptions struct {
	DeviceID   string
	CodeID     string
	CodeSecret string
	TargetUID  string
	Generation uint64
	Scope      []string
	ExpiresIn  time.Duration
}

func buildSessionRequestParams(options sessionRequestOptions, now time.Time) (map[string]any, error) {
	if options.DeviceID == "" || options.CodeID == "" || options.TargetUID == "" || options.Generation == 0 || len(options.Scope) == 0 {
		return nil, fmt.Errorf("需要 device_id、code_id、target_uid、generation 和 scope")
	}
	if strings.TrimSpace(options.CodeSecret) == "" {
		return nil, fmt.Errorf("需要通过 XALLOR_REMOTE_ACCESS_CODE 提供授权码")
	}
	params := map[string]any{"device_id": options.DeviceID, "code_id": options.CodeID, "code_secret": options.CodeSecret, "target_uid": options.TargetUID, "generation": options.Generation, "scope": options.Scope}
	if options.ExpiresIn > 0 {
		if now.IsZero() {
			return nil, fmt.Errorf("无效的 authority time")
		}
		params["session_expires_at"] = now.Add(options.ExpiresIn).UTC().Unix()
	}
	return params, nil
}

func cmdSession() *cobra.Command {
	return cmdSessionWith(ensureRuntime, rpc)
}

func cmdSessionWith(ensure func() error, call peerRPC) *cobra.Command {
	root := &cobra.Command{Use: "session", Short: "实验性逻辑授权会话（非 GUI WIP）"}
	var device, codeID, targetUID, sessionID, resumeID string
	var generation uint64
	var scope []string
	var expiresIn time.Duration
	request := &cobra.Command{Use: "request", Short: "通过本机 Runtime 请求接收方批准的 session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		params, err := buildSessionRequestParams(sessionRequestOptions{DeviceID: device, CodeID: codeID, CodeSecret: os.Getenv("XALLOR_REMOTE_ACCESS_CODE"), TargetUID: targetUID, Generation: generation, Scope: scope, ExpiresIn: expiresIn}, time.Now())
		if err != nil {
			return err
		}
		if err := ensure(); err != nil {
			return err
		}
		result, err := call("session.request", params)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	request.Flags().StringVar(&device, "device", "", "目标设备显示 ID")
	request.Flags().StringVar(&codeID, "code-id", "", "授权码 ID")
	request.Flags().StringVar(&targetUID, "target-uid", "", "目标 Runtime UID")
	request.Flags().Uint64Var(&generation, "generation", 0, "授权码 generation")
	request.Flags().StringSliceVar(&scope, "scope", nil, "请求操作范围")
	request.Flags().DurationVar(&expiresIn, "expires-in", 0, "session 有效时长；0 表示由 authority 决定")
	_ = request.MarkFlagRequired("device")
	_ = request.MarkFlagRequired("code-id")
	_ = request.MarkFlagRequired("target-uid")
	_ = request.MarkFlagRequired("generation")
	_ = request.MarkFlagRequired("scope")
	root.AddCommand(request)
	resume := &cobra.Command{Use: "resume", Short: "使用一次性 resume capability 恢复已批准 session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		capability := os.Getenv("XALLOR_REMOTE_RESUME_CAPABILITY")
		if device == "" || sessionID == "" || resumeID == "" || targetUID == "" || strings.TrimSpace(capability) == "" {
			return fmt.Errorf("需要 device、session-id、resume-id、target-uid 和 XALLOR_REMOTE_RESUME_CAPABILITY")
		}
		if err := ensure(); err != nil {
			return err
		}
		result, err := call("session.resume", map[string]any{"device_id": device, "session_id": sessionID, "resume_id": resumeID, "resume_capability": capability, "target_uid": targetUID})
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	resume.Flags().StringVar(&device, "device", "", "目标设备显示 ID")
	resume.Flags().StringVar(&sessionID, "session-id", "", "逻辑 session ID")
	resume.Flags().StringVar(&resumeID, "resume-id", "", "一次性 resume capability ID")
	resume.Flags().StringVar(&targetUID, "target-uid", "", "目标 Runtime UID")
	root.AddCommand(resume)
	return root
}
