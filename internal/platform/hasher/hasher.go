// Package hasher 提供 argon2id 口令哈希。
// 编码串采用 PHC 字符串格式，算法与成本参数随哈希一起存储，便于将来提升成本而不失效旧口令。
package hasher

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"

	"golang.org/x/crypto/argon2"
)

// 解析 PHC 编码串时可能遇到的失败。
var (
	ErrInvalidEncodedHash  = errors.New("hasher: invalid encoded hash format")
	ErrIncompatibleVersion = errors.New("hasher: incompatible argon2 version")
)

// Params 是 argon2id 的成本参数。
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams 返回项目约定的成本：64MiB 内存、3 次迭代、4 路并行。
func DefaultParams() Params {
	return Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// Hasher 负责口令的加密与校验。
type Hasher struct {
	params Params
}

// New 构造使用默认成本的 Hasher。
func New() *Hasher {
	return &Hasher{params: DefaultParams()}
}

// Make 生成形如 $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash> 的编码串。
func (h *Hasher) Make(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hasher: generate salt failed: %w", err)
	}

	key := argon2.IDKey(
		[]byte(password),
		salt,
		h.params.Iterations,
		h.params.Memory,
		h.params.Parallelism,
		h.params.KeyLength,
	)

	encoding := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.Memory,
		h.params.Iterations,
		h.params.Parallelism,
		encoding.EncodeToString(salt),
		encoding.EncodeToString(key),
	), nil
}

// Check 校验口令是否与编码串匹配；编码串损坏时返回错误而非 false。
func (h *Hasher) Check(encoded, password string) (bool, error) {
	params, salt, expected, err := decode(encoded)
	if err != nil {
		return false, err
	}

	keyLength, err := checkedLength(len(expected))
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		keyLength,
	)
	// 常量时间比较，避免通过响应耗时侧信道推断口令。
	return subtle.ConstantTimeCompare(expected, actual) == 1, nil
}

// checkedLength 把字节切片长度安全地转成 uint32。
// 这些切片长度实际上不可能溢出，显式校验是为了不让 gosec G115 把它当成静默截断。
func checkedLength(value int) (uint32, error) {
	if value < 0 || int64(value) > math.MaxUint32 {
		return 0, ErrInvalidEncodedHash
	}
	return uint32(value), nil
}

// decode 拆解 PHC 编码串，返回其中的成本参数、盐值与摘要。
func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidEncodedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, ErrInvalidEncodedHash
	}
	if version != argon2.Version {
		return Params{}, nil, nil, ErrIncompatibleVersion
	}

	var params Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return Params{}, nil, nil, ErrInvalidEncodedHash
	}

	encoding := base64.RawStdEncoding
	salt, err := encoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidEncodedHash
	}
	key, err := encoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidEncodedHash
	}

	saltLength, err := checkedLength(len(salt))
	if err != nil {
		return Params{}, nil, nil, err
	}
	keyLength, err := checkedLength(len(key))
	if err != nil {
		return Params{}, nil, nil, err
	}

	params.SaltLength = saltLength
	params.KeyLength = keyLength
	return params, salt, key, nil
}
