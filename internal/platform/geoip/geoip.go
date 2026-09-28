// Package geoip 提供基于 lionsoul2014/ip2region 的离线 IP 归属地极速检索服务、无锁热重载与优雅降级保障。
package geoip

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/rs/zerolog"

	"bgin/internal/config"
)

// fallbackLogger 是 geoip 包在装配期未注入日志器时的兜底输出。
// 显式复用包级实例而非 zerolog 全局单例，避免绕过应用配置的日志通道。
var fallbackLogger = zerolog.New(os.Stderr).With().Timestamp().Logger()

const (
	// internalIP 表示内网/局域网/环回地址的默认标识。
	internalIP = "Internal IP"
	// unknownLocation 表示无法识别或未加载数据库时的缺省归属地描述。
	unknownLocation = "Unknown"
	// ipv6Default 表示公网 IPv6 地址的缺省归属地描述。
	ipv6Default = "IPv6"
)

// addrKind 定义 IP 地址分类枚举。
type addrKind int

const (
	addrKindPublicIPv4 addrKind = iota
	addrKindInternal
	addrKindIPv6
)

// Location 封装结构化的 IP 物理与网络归属地信息。
type Location struct {
	Country  string `json:"country,omitempty"`
	Region   string `json:"region,omitempty"`
	Province string `json:"province,omitempty"`
	City     string `json:"city,omitempty"`
	ISP      string `json:"isp,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

var (
	internalLocation = Location{
		Country: internalIP,
		City:    internalIP,
		Raw:     "0|0|0|" + internalIP + "|" + internalIP,
	}
	unknownLocationModel = Location{
		Country: unknownLocation,
		Raw:     "0|0|0|" + unknownLocation + "|" + unknownLocation,
	}
	ipv6Location = Location{
		Country: ipv6Default,
		City:    ipv6Default,
		Raw:     "0|0|0|" + ipv6Default + "|" + ipv6Default,
	}
)

func isInternalAddr(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()
}

func parseIPString(ipStr string) (netip.Addr, error) {
	trimmed := strings.TrimSpace(ipStr)
	if trimmed == "" {
		return netip.Addr{}, errors.New("empty ip address")
	}

	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid ip address: %s", trimmed)
	}

	return addr, nil
}

func classifyAddr(addr netip.Addr) (addrKind, error) {
	if !addr.IsValid() {
		return addrKindPublicIPv4, errors.New("invalid ip address")
	}
	if isInternalAddr(addr) {
		return addrKindInternal, nil
	}
	if addr.Is6() {
		return addrKindIPv6, nil
	}
	return addrKindPublicIPv4, nil
}

func resolveDBPath(defaultPath string, overrides ...string) string {
	if len(overrides) > 0 && strings.TrimSpace(overrides[0]) != "" {
		return strings.TrimSpace(overrides[0])
	}
	return defaultPath
}

// FormatOption 配置归属地文本输出格式的函数式选项。
type FormatOption func(*formatConfig)

type formatConfig struct {
	separator  string
	excludeISP bool
}

// WithSeparator 指定地域各层级之间的连接分隔符（默认 "·"）。
func WithSeparator(sep string) FormatOption {
	return func(c *formatConfig) {
		c.separator = sep
	}
}

// WithoutISP 排除运营商信息，仅展示国家/省份/城市等物理地理信息。
func WithoutISP() FormatOption {
	return func(c *formatConfig) {
		c.excludeISP = true
	}
}

// Format 根据指定的格式化选项输出结构化的地域描述文本。
func (l Location) Format(opts ...FormatOption) string {
	cfg := formatConfig{
		separator: "·",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	parts := l.collectParts(cfg.excludeISP)
	if len(parts) == 0 {
		return unknownLocation
	}
	return strings.Join(parts, cfg.separator)
}

func (l Location) collectParts(excludeISP bool) []string {
	fields := []string{l.Country, l.Province, l.City}
	if !excludeISP {
		fields = append(fields, l.ISP)
	}

	var parts []string
	for _, raw := range fields {
		val := strings.TrimSpace(raw)
		if val == "" || val == "0" {
			continue
		}
		if len(parts) > 0 && parts[len(parts)-1] == val {
			continue
		}
		parts = append(parts, val)
	}
	return parts
}

// Summary 输出默认格式的地域与运营商精简摘要（例如 "China·Shanghai·Telecom"、"Internal IP"）。
func (l Location) Summary() string {
	return l.Format()
}

// ParseRaw 将 ip2region 原始管道符输出格式（Country|Region|Province|City|ISP）反解为 Location。
func ParseRaw(raw string) Location {
	parts := strings.Split(raw, "|")
	loc := Location{Raw: raw}
	if len(parts) >= 5 {
		loc.Country = sanitizePart(parts[0])
		loc.Region = sanitizePart(parts[1])
		loc.Province = sanitizePart(parts[2])
		loc.City = sanitizePart(parts[3])
		loc.ISP = sanitizePart(parts[4])
	}
	return loc
}

func sanitizePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "0" {
		return ""
	}
	return s
}

// Service 定义 IP 归属地检索服务的标准契约。
type Service interface {
	// Lookup 解析指定 IP 字符串并返回结构化归属地数据。
	Lookup(ip string) (*Location, error)
	// LookupAddr 解析指定 netip.Addr 并返回结构化归属地数据（零堆分配快速路径）。
	LookupAddr(addr netip.Addr) (*Location, error)
	// Summary 便捷获取指定 IP 的人性化地域描述字符串，支持函数式格式化选项。
	Summary(ip string, opts ...FormatOption) string
	// SummaryAddr 便捷获取指定 netip.Addr 的人性化地域描述字符串。
	SummaryAddr(addr netip.Addr, opts ...FormatOption) string
	// Reload 重新加载 IP 归属地数据库（支持热重载，可选指定新路径）。
	Reload(dbPath ...string) error
	// Close 释放检索器占用的内存与文件句柄。
	Close()
}

// xdbEngine 封装单份只读 XDB 内存缓冲区与并发安全的检索器对象池。
type xdbEngine struct {
	dbPath  string
	version *xdb.Version
	cBuff   []byte
	pool    sync.Pool
}

func newEngine(dbPath string, cBuff []byte) (*xdbEngine, error) {
	header, err := xdb.LoadHeaderFromBuff(cBuff)
	if err != nil {
		return nil, fmt.Errorf("failed to load ip2region header: %w", err)
	}

	version, err := xdb.VersionFromHeader(header)
	if err != nil {
		return nil, fmt.Errorf("failed to get version from ip2region header: %w", err)
	}

	eng := &xdbEngine{
		dbPath:  dbPath,
		version: version,
		cBuff:   cBuff,
	}

	eng.pool.New = func() any {
		searcher, err := xdb.NewWithBuffer(eng.version, eng.cBuff)
		if err != nil {
			fallbackLogger.Error().Err(err).Msg("failed to create pooled ip2region searcher")
			return nil
		}
		return searcher
	}

	return eng, nil
}

func loadEngine(path string) (*xdbEngine, error) {
	cBuff, err := xdb.LoadContentFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load geoip xdb content: %w", err)
	}
	return newEngine(path, cBuff)
}

func (e *xdbEngine) search(ipBytes []byte) (string, error) {
	obj := e.pool.Get()
	if obj == nil {
		return "", errors.New("searcher pool returned nil")
	}
	searcher, ok := obj.(*xdb.Searcher)
	if !ok {
		return "", errors.New("invalid searcher type in pool")
	}
	defer e.pool.Put(searcher)

	return searcher.Search(ipBytes)
}

// xdbService 基于 ip2region 内存全缓冲模式与 atomic.Pointer 无锁热重载的检索服务实现。
type xdbService struct {
	cfg    config.GeoIPConfig
	engine atomic.Pointer[xdbEngine]
}

// Lookup 执行 IP 字符串地址检索。
func (s *xdbService) Lookup(ipStr string) (*Location, error) {
	addr, err := parseIPString(ipStr)
	if err != nil {
		return nil, err
	}
	return s.LookupAddr(addr)
}

// LookupAddr 执行 netip.Addr 原生地址检索（零堆分配快速路径）。
func (s *xdbService) LookupAddr(addr netip.Addr) (*Location, error) {
	unmapped := addr.Unmap()
	kind, err := classifyAddr(unmapped)
	if err != nil {
		return nil, err
	}

	switch kind {
	case addrKindInternal:
		return &internalLocation, nil
	case addrKindIPv6:
		return &ipv6Location, nil
	case addrKindPublicIPv4:
	}

	eng := s.engine.Load()
	if eng == nil {
		return &unknownLocationModel, nil
	}

	ip4 := unmapped.As4()
	raw, err := eng.search(ip4[:])
	if err != nil {
		return nil, fmt.Errorf("ip2region search failed: %w", err)
	}

	parsed := ParseRaw(raw)
	return &parsed, nil
}

// Summary 获取指定 IP 的精简地域描述。
func (s *xdbService) Summary(ipStr string, opts ...FormatOption) string {
	loc, err := s.Lookup(ipStr)
	if err != nil || loc == nil {
		return unknownLocation
	}
	return loc.Format(opts...)
}

// SummaryAddr 获取指定 netip.Addr 的精简地域描述。
func (s *xdbService) SummaryAddr(addr netip.Addr, opts ...FormatOption) string {
	loc, err := s.LookupAddr(addr)
	if err != nil || loc == nil {
		return unknownLocation
	}
	return loc.Format(opts...)
}

// Reload 无锁原子替换底层检索器（零停机热重载）。
func (s *xdbService) Reload(dbPaths ...string) error {
	targetPath := resolveDBPath(s.cfg.DBPath, dbPaths...)
	if _, err := os.Stat(targetPath); err != nil {
		return fmt.Errorf("geoip database file stat failed: %w", err)
	}

	eng, err := loadEngine(targetPath)
	if err != nil {
		return err
	}

	s.engine.Store(eng)
	return nil
}

// Close 释放检索服务底层引用。
func (s *xdbService) Close() {
	s.engine.Store(nil)
}

// noopService 当数据库文件不存在或配置关闭时的降级空实现。
type noopService struct{}

// Lookup 返回内网、IPv6 或默认未知归属地。
func (n *noopService) Lookup(ipStr string) (*Location, error) {
	addr, err := parseIPString(ipStr)
	if err != nil {
		return nil, err
	}
	return n.LookupAddr(addr)
}

// LookupAddr 返回基于 netip.Addr 的分类归属地。
func (n *noopService) LookupAddr(addr netip.Addr) (*Location, error) {
	kind, err := classifyAddr(addr.Unmap())
	if err != nil {
		return nil, err
	}

	switch kind {
	case addrKindInternal:
		return &internalLocation, nil
	case addrKindIPv6:
		return &ipv6Location, nil
	case addrKindPublicIPv4:
		return &unknownLocationModel, nil
	default:
		return &unknownLocationModel, nil
	}
}

// Summary 获取降级模式下的地域描述。
func (n *noopService) Summary(ipStr string, opts ...FormatOption) string {
	loc, err := n.Lookup(ipStr)
	if err != nil || loc == nil {
		return unknownLocation
	}
	return loc.Format(opts...)
}

// SummaryAddr 获取降级模式下的 netip.Addr 地域描述。
func (n *noopService) SummaryAddr(addr netip.Addr, opts ...FormatOption) string {
	loc, err := n.LookupAddr(addr)
	if err != nil || loc == nil {
		return unknownLocation
	}
	return loc.Format(opts...)
}

// Reload 空操作。
func (n *noopService) Reload(_ ...string) error {
	return nil
}

// Close 空操作。
func (n *noopService) Close() {}

// NewNoop 创建安全的降级空实现。
func NewNoop() Service {
	return &noopService{}
}

// NewService 根据配置构建 GeoIP 服务实例。若数据库文件缺失则自动启用安全降级。
func NewService(cfg config.GeoIPConfig) (Service, error) {
	if !cfg.Enabled {
		return NewNoop(), nil
	}

	if _, err := os.Stat(cfg.DBPath); os.IsNotExist(err) {
		fallbackLogger.Warn().
			Str("path", cfg.DBPath).
			Msg("geoip database file does not exist, degrading to noop service")
		return NewNoop(), nil
	}

	eng, err := loadEngine(cfg.DBPath)
	if err != nil {
		fallbackLogger.Warn().
			Err(err).
			Str("path", cfg.DBPath).
			Msg("failed to load geoip xdb engine, degrading to noop service")
		return NewNoop(), nil
	}

	svc := &xdbService{cfg: cfg}
	svc.engine.Store(eng)
	return svc, nil
}
