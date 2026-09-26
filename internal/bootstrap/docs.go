package bootstrap

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"bgin/apidocs"
)

// 文档页与规范的路径。定义在一处：路由、页面内的引用、启动日志共用同一份，避免漂移。
const (
	// docsPagePath 是 Scalar 文档页的路径。
	docsPagePath = "/docs"
	// referenceSpecPath 是浏览器拉取规范的路径。
	referenceSpecPath = docsPagePath + "/openapi.json"
)

// referencePage 是 Scalar API Reference 的宿主页。
//
// 与 Swagger UI 不同，Scalar 不需要服务端渲染：整页只是一个 ES module 引导，
// UI 本体由浏览器从 jsDelivr 拉取。代价是访问 /docs 的浏览器必须能连到公网 CDN；
// 纯内网环境要么放行该域名，要么把 @scalar/api-reference 的构建产物自行内嵌。
//
// 规范路径由 referenceSpecPath 拼接而来，避免同一路径写在两处后悄悄漂移。
const referencePage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>bgin API</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module">
      import { createApiReference } from 'https://cdn.jsdelivr.net/npm/@scalar/api-reference/esm.js'

      const spec = await fetch('` + referenceSpecPath + `').then(r => r.json()).catch(() => ({}))

      const SCALAR_UNIFIED_CONFIG = {
        layout: 'modern',
        theme: 'purple',
        withDefaultFonts: true,
        hideDarkModeToggle: false,
        hideModels: true,

        // Layout & title settings
        operationTitleSource: 'summary',
        showOperationId: false,
        defaultOpenFirstTag: true,
        defaultOpenAllTags: false,
        orderRequiredPropertiesFirst: true,
        orderSchemaPropertiesBy: 'preserve',
        expandAllModelSections: false,
        expandAllResponses: false,
        expandAllSchemaProperties: false,

        // Developer experience
        persistAuth: true,
        hideTestRequestButton: false,
        hideClientButton: false,
        hideSearch: false,
        showDeveloperTools: 'localhost',
        showToolbar: 'localhost',
        documentDownloadType: 'both',

        // Privacy & security
        telemetry: false,
        isEditable: false,
        _integration: 'nestjs',

        // Introduction section full-width layout & custom styles
        customCss: [
          '.introduction-section .section-columns {',
          '  flex-direction: column !important;',
          '  gap: 32px !important;',
          '}',
          '.introduction-section .section-column {',
          '  max-width: 100% !important;',
          '  width: 100% !important;',
          '}',
          '.introduction-section .sticky-cards {',
          '  position: static !important;',
          '  top: auto !important;',
          '  max-width: 100% !important;',
          '  width: 100% !important;',
          '}',
          '.introduction-section .introduction-card {',
          '  display: grid !important;',
          '  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)) !important;',
          '  gap: 16px !important;',
          '}',
          '.introduction-section .introduction-card .scalar-reference-intro-clients {',
          '  grid-column: 1 / -1 !important;',
          '}',
          '',
          '/* Hide sidebar top Ask AI button */',
          'div[class*="gap-1.5"][class*="px-3"] > button:not(:first-of-type),',
          '.flex.gap-1\\.5.px-3.pt-3 > button:not(:first-of-type) {',
          '  display: none !important;',
          '}',
          '',
          '/* Hide sidebar bottom Powered by Scalar attribution link */',
          'a[href="https://www.scalar.com"],',
          'a[href="https://www.scalar.com/"],',
          '.scalar-version-number {',
          '  display: none !important;',
          '}',
        ].join('\n'),
      }

      createApiReference('#app', {
        ...SCALAR_UNIFIED_CONFIG,
        url: '` + referenceSpecPath + `',
        content: spec,
        tagsSorter: (a, b) => {
          const getTagOrder = (tag) => {
            if (tag?.['x-order']) return Number(tag['x-order'])
            const found = spec?.tags?.find(t => t.name === tag?.name)
            if (found?.['x-order']) return Number(found['x-order'])
            return 999
          }
          const orderA = getTagOrder(a)
          const orderB = getTagOrder(b)
          if (orderA !== orderB) return orderA - orderB
          return (a.name || '').localeCompare(b.name || '')
        },
        operationsSorter: (a, b) => {
          const getOrder = (item) => {
            const p = item.path || ''
            const m = (item.method || item.httpVerb || '').toLowerCase()
            return Number(spec?.paths?.[p]?.[m]?.['x-order'] ?? 999)
          }
          const orderA = getOrder(a)
          const orderB = getOrder(b)
          if (orderA !== orderB) return orderA - orderB
          return (a.path || '').localeCompare(b.path || '')
        },
      })
    </script>
  </body>
</html>
`

// registerDocs 挂载 API 文档，仅在 ENABLE_DOCS 开启时调用。
//
// 这里用两条静态路由而不是 gin-swagger 那种 catch-all：规范文件与页面都在 /docs 之下，
// 而 gin 不允许静态路径与 catch-all 通配同级（注册时会 panic）。
func registerDocs(engine *gin.Engine) {
	engine.GET(docsPagePath, serveReference)
	engine.GET(referenceSpecPath, serveReferenceSpec)
}

// serveReference 返回 Scalar 的宿主页。
func serveReference(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(referencePage))
}

// serveReferenceSpec 原样返回编进二进制的规范，不做任何转换。
// Scalar 自己会在加载时把 Swagger 2.0 升级为 OpenAPI 3.x。
func serveReferenceSpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", apidocs.Spec)
}
