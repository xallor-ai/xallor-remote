package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/relay"
)

func cmdRelayPrincipal(data *string) *cobra.Command {
	c := &cobra.Command{Use: "principal", Short: "管理中转上的调用身份（需本机数据目录权限）"}
	run := func(cmd *cobra.Command, action func(context.Context, *account.Service) (any, error)) error {
		if *data == "" {
			return fmt.Errorf("需要 --data 指定中转数据目录")
		}
		s, err := relay.OpenSQLStore(*data)
		if err != nil {
			return err
		}
		defer s.Close()
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		result, err := action(ctx, account.New(s))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}
	var name, kind, tenant string
	var ttl time.Duration
	create := &cobra.Command{Use: "create", Short: "创建调用身份并签发凭据", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, func(ctx context.Context, service *account.Service) (any, error) {
				return service.CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: name, Kind: kind, TenantID: tenant, TTL: ttl})
			})
		}}
	create.Flags().StringVar(&name, "name", "", "名称")
	create.Flags().StringVar(&kind, "kind", "user", "user 或 service")
	create.Flags().StringVar(&tenant, "tenant", "", "已有空间 ID；省略则创建个人空间")
	create.Flags().DurationVar(&ttl, "ttl", 24*time.Hour, "凭据有效期")
	var credential string
	revoke := &cobra.Command{Use: "revoke", Short: "撤销身份凭据", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, func(ctx context.Context, service *account.Service) (any, error) {
				return map[string]bool{"revoked": true}, service.RevokeCredential(ctx, credential)
			})
		}}
	revoke.Flags().StringVar(&credential, "credential", "", "凭据 ID")
	_ = revoke.MarkFlagRequired("credential")
	var membership string
	var active bool
	member := &cobra.Command{Use: "membership", Short: "启用或停用成员身份", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, func(ctx context.Context, service *account.Service) (any, error) {
				return map[string]bool{"active": active}, service.SetMembershipActive(ctx, membership, active)
			})
		}}
	member.Flags().StringVar(&membership, "membership", "", "成员关系 ID")
	member.Flags().BoolVar(&active, "active", true, "是否启用")
	_ = member.MarkFlagRequired("membership")
	_ = member.MarkFlagRequired("active")
	var organizationName, issueMember string
	var issueTTL time.Duration
	organization := &cobra.Command{Use: "space", Short: "创建团队空间", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, func(ctx context.Context, service *account.Service) (any, error) {
			id, err := service.CreateOrganization(ctx, organizationName)
			return map[string]string{"tenant_id": id}, err
		})
	}}
	organization.Flags().StringVar(&organizationName, "name", "", "空间名称")
	issue := &cobra.Command{Use: "issue", Short: "为现有成员签发新凭据", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, func(ctx context.Context, service *account.Service) (any, error) {
			return service.IssueCredential(ctx, account.IssueCredentialRequest{MembershipID: issueMember, TTL: issueTTL})
		})
	}}
	issue.Flags().StringVar(&issueMember, "membership", "", "成员关系 ID")
	issue.Flags().DurationVar(&issueTTL, "ttl", 24*time.Hour, "凭据有效期")
	_ = issue.MarkFlagRequired("membership")
	c.AddCommand(create, revoke, member, organization, issue)
	return c
}

func cmdAccount() *cobra.Command {
	c := &cobra.Command{Use: "account", Short: "当前 Runtime 的调用身份"}
	var file string
	use := &cobra.Command{Use: "use", Short: "导入身份凭据并断开旧的控制连接", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, err := os.Stat(file)
			if err != nil {
				return err
			}
			if info.Size() > 16*1024 {
				return fmt.Errorf("凭据文件过大")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var enrollment principal.Enrollment
			if json.Unmarshal(raw, &enrollment) != nil || !principal.ValidToken(enrollment.Token) {
				return fmt.Errorf("凭据文件无效")
			}
			if err := ensureRuntime(); err != nil {
				return err
			}
			_, err = rpc("account.set", map[string]any{"token": enrollment.Token})
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "身份凭据已导入")
			}
			return err
		}}
	use.Flags().StringVar(&file, "credential-file", "", "签发的 JSON 凭据文件")
	_ = use.MarkFlagRequired("credential-file")
	clear := &cobra.Command{Use: "clear", Short: "清除身份凭据并断开旧的控制连接", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := ensureRuntime(); err != nil {
			return err
		}
		_, err := rpc("account.set", map[string]any{"token": ""})
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "身份凭据已清除")
		}
		return err
	}}
	status := &cobra.Command{Use: "status", Short: "查看身份凭据是否已配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := ensureRuntime(); err != nil {
			return err
		}
		result, err := rpc("account.status", nil)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	c.AddCommand(use, clear, status)
	return c
}
