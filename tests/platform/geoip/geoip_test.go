// Package geoip_test 验证离线 IP 归属地解析与安全降级服务契约。
package geoip_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/geoip"
)

func TestNoopService(t *testing.T) {
	svc := geoip.NewNoop()
	require.NotNil(t, svc)

	// 1. 环回与局域网私有地址识别
	loc, err := svc.Lookup("127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "Internal IP", loc.Country)
	assert.Equal(t, "Internal IP", loc.City)

	loc, err = svc.Lookup("192.168.1.100")
	require.NoError(t, err)
	assert.Equal(t, "Internal IP", loc.Country)

	loc, err = svc.Lookup("::1")
	require.NoError(t, err)
	assert.Equal(t, "Internal IP", loc.Country)

	// 2. IPv6 地址识别
	loc, err = svc.Lookup("2400:3200::1")
	require.NoError(t, err)
	assert.Equal(t, "IPv6", loc.Country)

	// 3. 公网 IPv4 在无数据库时降级为 Unknown
	loc, err = svc.Lookup("8.8.8.8")
	require.NoError(t, err)
	assert.Equal(t, "Unknown", loc.Country)

	// 4. 非法 IP 格式返回错误
	_, err = svc.Lookup("not-an-ip")
	require.Error(t, err)

	// 5. 零堆分配快速路径 LookupAddr
	addr := netip.MustParseAddr("10.0.0.1")
	loc, err = svc.LookupAddr(addr)
	require.NoError(t, err)
	assert.Equal(t, "Internal IP", loc.Country)

	// 6. Summary 与 SummaryAddr
	assert.Equal(t, "Internal IP", svc.Summary("127.0.0.1"))
	assert.Equal(t, "Internal IP", svc.SummaryAddr(addr))
	assert.Equal(t, "Unknown", svc.Summary("invalid"))

	// 7. Reload 与 Close 空操作安全调用
	require.NoError(t, svc.Reload())
	svc.Close()
}

func TestLocationFormatAndParseRaw(t *testing.T) {
	raw := "中国|0|上海|上海市|电信"
	loc := geoip.ParseRaw(raw)

	assert.Equal(t, "中国", loc.Country)
	assert.Empty(t, loc.Region)
	assert.Equal(t, "上海", loc.Province)
	assert.Equal(t, "上海市", loc.City)
	assert.Equal(t, "电信", loc.ISP)
	assert.Equal(t, raw, loc.Raw)

	// 默认 Summary 拼接（去重相邻相同名称）
	assert.Equal(t, "中国·上海·上海市·电信", loc.Summary())

	// 自定义分隔符
	assert.Equal(t, "中国-上海-上海市-电信", loc.Format(geoip.WithSeparator("-")))

	// 排除 ISP
	assert.Equal(t, "中国·上海·上海市", loc.Format(geoip.WithoutISP()))
}

func TestNewServiceFallback(t *testing.T) {
	// 1. 未开启时返回降级实例
	svc, err := geoip.NewService(config.GeoIPConfig{
		Enabled: false,
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)

	// 2. 开启但数据库文件不存在时安全降级
	svc, err = geoip.NewService(config.GeoIPConfig{
		Enabled: true,
		DBPath:  "/nonexistent/ip2region.xdb",
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.Equal(t, "Internal IP", svc.Summary("127.0.0.1"))
}
