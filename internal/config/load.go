package config

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const dotenvPath = ".env"

// Load 从环境变量装载并校验全部应用配置。
func Load() (*Config, error) {
	if err := loadDotEnv(dotenvPath); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: parse failed: %w", err)
	}
	cfg.Database.Debug = cfg.ResolveDBDebug()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Default 返回一份最小可用配置，供「配置装载失败」时的自省类命令使用
// （例如 route list 需要一份配置才能构建路由表）。
//
// 它既未经校验也不含任何真实连接信息，因此严禁用于启动服务；
// 与 InspectEngine 的 nil 校验配合，避免出现"静默伪造配置"的隐性路径。
func Default() *Config {
	return &Config{
		App: AppConfig{
			Name: "bgin",
			Env:  "local",
		},
	}
}

// loadDotEnv 尝试装载本地 .env 文件至进程环境，文件不存在时不报错。
func loadDotEnv(path string) error {
	err := godotenv.Load(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("config: load %s failed: %w", path, err)
}
