package config

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// dotenvPath 是本地敏感值的来源，通常不入库。
const dotenvPath = ".env"

// Load 装配配置并校验：先把 .env 补进进程环境，再逐字段解析，最后做标签表达不了的检查。
// 任何一步失败都立即返回，不做带病启动。
func Load() (*Config, error) {
	if err := loadDotEnv(dotenvPath); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: parse failed: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadDotEnv 把 .env 里的变量补进进程环境：只填空缺、不覆盖已有值（godotenv.Load 的语义），
// 因此进程环境始终优先于文件。
//
// 文件不存在属于正常情况——容器里通常只注入环境变量。其余错误必须返回：被吞掉的格式错误会
// 表现为"一批必填项突然缺失"，让人往环境没配好的方向排查，而真正的原因是文件本身。
func loadDotEnv(path string) error {
	err := godotenv.Load(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("config: load %s failed: %w", path, err)
}
