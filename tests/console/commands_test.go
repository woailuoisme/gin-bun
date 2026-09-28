package console_test

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/console"
)

// TestRootCommand_Registration 验证所有预期的 CLI 命令均已在根命令中注册（包括层级子树与隐藏别名）。
func TestRootCommand_Registration(t *testing.T) {
	cmd := console.NewRootCommand()
	require.NotNil(t, cmd)

	// 1. 验证现代树状一级命令
	expectedTopCommands := []string{
		"serve",
		"queue",
		"route",
		"db",
		"scout",
		"jwt",
		"make",
	}

	commandNames := lo.Map(cmd.Commands(), func(c *cobra.Command, _ int) string {
		return c.Name()
	})

	for _, expected := range expectedTopCommands {
		assert.Contains(t, commandNames, expected, "Root command should contain top-level command: "+expected)
	}

	// 2. 验证各个子命令树的挂载完整性
	assertSubcommandExists(t, cmd, "route", []string{"list"})
	assertSubcommandExists(t, cmd, "db", []string{"migrate", "seed", "fresh"})
	assertSubcommandExists(t, cmd, "queue", []string{"work", "failed", "retry", "flush"})
	assertSubcommandExists(t, cmd, "scout", []string{"import", "flush"})
	assertSubcommandExists(t, cmd, "jwt", []string{"secret"})
	assertSubcommandExists(t, cmd, "make", []string{"crud", "destroy"})

	// 3. 验证向后兼容的老 Artisan 冒号别名命令（Hidden: true）
	expectedColonAliases := []string{
		"route:list",
		"db:migrate",
		"db:seed",
		"db:fresh",
		"queue:work",
		"queue:failed",
		"queue:retry",
		"queue:flush",
		"scout:import",
		"scout:flush",
		"jwt:secret",
		"make:crud",
		"make:destroy",
	}

	for _, alias := range expectedColonAliases {
		assert.Contains(t, commandNames, alias, "Root command should contain hidden alias: "+alias)
	}
}

// assertSubcommandExists 辅助校验父命令下的子命令集合。
func assertSubcommandExists(t *testing.T, root *cobra.Command, parentName string, expectedSubs []string) {
	t.Helper()
	parentCmd, _, err := root.Find([]string{parentName})
	require.NoError(t, err)
	require.NotNil(t, parentCmd)

	subNames := lo.Map(parentCmd.Commands(), func(c *cobra.Command, _ int) string {
		return c.Name()
	})

	for _, sub := range expectedSubs {
		assert.Contains(t, subNames, sub, parentName+" should contain subcommand: "+sub)
	}
}

// TestParseQueueWeights 验证队列权重参数解析。
func TestParseQueueWeights(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]int
	}{
		{
			name:     "empty input defaults to default:1",
			input:    "",
			expected: map[string]int{"default": 1},
		},
		{
			name:     "single queue name without weight",
			input:    "critical",
			expected: map[string]int{"critical": 1},
		},
		{
			name:     "single queue with weight",
			input:    "critical:5",
			expected: map[string]int{"critical": 5},
		},
		{
			name:     "multiple queues with weights and whitespace",
			input:    "high:3, default:1, low:1 ",
			expected: map[string]int{"high": 3, "default": 1, "low": 1},
		},
		{
			name:     "invalid weight falls back to 1",
			input:    "custom:invalid",
			expected: map[string]int{"custom": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := console.ParseQueueWeights(tc.input)
			assert.Equal(t, tc.expected, res)
		})
	}
}

// TestCommand_ValidationErrors 验证命令在缺少必填入参时的防御性报错（双模调用验证：冒号别名与层级子命令）。
func TestCommand_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("queue:retry without arg (alias)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"queue:retry"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please provide a job ID or 'all'")
	})

	t.Run("queue retry without arg (native tree)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"queue", "retry"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please provide a job ID or 'all'")
	})

	t.Run("scout:import without model or --all (alias)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"scout:import"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please specify a model name or use --all")
	})

	t.Run("scout import without model or --all (native tree)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"scout", "import"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please specify a model name or use --all")
	})

	t.Run("scout:flush without model (alias)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"scout:flush"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please provide a model name to flush")
	})

	t.Run("scout flush without model (native tree)", func(t *testing.T) {
		cmd := console.NewRootCommand()
		cmd.SetArgs([]string{"scout", "flush"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please provide a model name to flush")
	})
}
