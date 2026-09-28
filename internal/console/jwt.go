package console

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"bgin/internal/console/core"
)

// jwtSecretRegexp 匹配 .env 中已存在的 JWT_SECRET 配置行。
var jwtSecretRegexp = regexp.MustCompile(`(?m)^JWT_SECRET=(.*)$`)

// newJWTSecretCmd 创建或更新 .env 中的 JWT_SECRET 签名密钥（仿照 Laravel artisan jwt:secret）。
func newJWTSecretCmd() *cobra.Command {
	var show bool
	var force bool

	cmd := &cobra.Command{
		Use:   "jwt:secret",
		Short: "Set the JWT secret key in .env file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 1. 生成 32 字节高强度随机密钥并做 base64 编码 (44 字符，完全满足 >= 32 位安全要求)
			secret, err := GenerateRandomSecret(32)
			if err != nil {
				return fmt.Errorf("failed to generate secret: %w", err)
			}

			if show {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), secret)
				return nil
			}

			// 2. 检查 .env 文件是否存在；不存在时若有 .env.example 则拷贝一份作为起点
			envPath := ".env"
			ensureEnvFile(cmd, envPath)

			// 3. 读取 .env 内容
			content, err := os.ReadFile(envPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to read %s: %w", envPath, err)
			}

			strContent := string(content)
			matches := jwtSecretRegexp.FindStringSubmatch(strContent)

			// 若已存在且非空，未加 --force 时需进行交互确认
			if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" && !force {
				confirmed, err := core.Confirm("A valid JWT_SECRET already exists in .env. Overwrite it?",
					"This will invalidate all previously issued tokens.",
					"Yes, overwrite")
				if err != nil {
					return err
				}
				if !confirmed {
					core.Warn(cmd, "JWT secret update cancelled.")
					return nil
				}
			}

			// 4. 替换现有键值或在末尾追加
			var newContent string
			if jwtSecretRegexp.MatchString(strContent) {
				newContent = jwtSecretRegexp.ReplaceAllString(strContent, "JWT_SECRET="+secret)
			} else {
				if len(strContent) > 0 && !strings.HasSuffix(strContent, "\n") {
					strContent += "\n"
				}
				newContent = strContent + "JWT_SECRET=" + secret + "\n"
			}

			if err := os.WriteFile(envPath, []byte(newContent), 0o600); err != nil {
				return fmt.Errorf("failed to write %s: %w", envPath, err)
			}

			core.Success(cmd, "JWT secret [%s] set successfully.", secret)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&show, "show", "s", false, "Display the key instead of modifying files")
	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, "Skip confirmation prompt when overwriting an existing key")

	return cmd
}

// GenerateRandomSecret 生成指定字节长度的高强度随机密钥，以标准 base64 字符串形式返回。
func GenerateRandomSecret(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ensureEnvFile 检查目标环境文件是否存在，若不存在则尝试从 .env.example 复制一份作为起点。
func ensureEnvFile(cmd *cobra.Command, envPath string) {
	if _, err := os.Stat(envPath); !errors.Is(err, os.ErrNotExist) {
		return
	}
	exampleContent, err := os.ReadFile(".env.example")
	if err != nil {
		return
	}
	if err := os.WriteFile(envPath, exampleContent, 0o600); err == nil {
		core.Title(cmd, ".env file not found, created from .env.example.")
	}
}
