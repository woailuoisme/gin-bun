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

// Default 返回一份最小可用默认配置，供配置装载失败时的路由自省类命令使用。
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
