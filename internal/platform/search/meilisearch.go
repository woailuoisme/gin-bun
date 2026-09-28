package search

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/samber/oops"

	"bgin/internal/platform/errs"
)

type meilisearchClient struct {
	manager meilisearch.ServiceManager
	prefix  string
}

var (
	_ Client = (*meilisearchClient)(nil)
	_ Index  = (*meilisearchIndex)(nil)
)

// NewMeilisearchClient 从已有的 Meilisearch ServiceManager 创建客户端门面。
func NewMeilisearchClient(manager meilisearch.ServiceManager, prefix ...string) Client {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	return &meilisearchClient{
		manager: manager,
		prefix:  p,
	}
}

// NewClientFromConfig 根据配置创建 Meilisearch 官方客户端适配门面。
func NewClientFromConfig(cfg Config) (Client, error) {
	host := strings.TrimSpace(cfg.Meilisearch.Host)
	apiKey := strings.TrimSpace(cfg.Meilisearch.APIKey)
	if !cfg.Enabled() || host == "" {
		return NewNoop(), nil
	}

	client := meilisearch.New(
		host,
		meilisearch.WithAPIKey(apiKey),
		meilisearch.WithCustomClient(&http.Client{
			Timeout: 5 * time.Second,
		}),
	)

	return NewMeilisearchClient(client, cfg.Prefix), nil
}

func (c *meilisearchClient) Index(name string) Index {
	prefixed := c.prefix + name
	return &meilisearchIndex{
		name:         name,
		prefixedName: prefixed,
		manager:      c.manager.Index(prefixed),
	}
}

func (c *meilisearchClient) Enabled() bool {
	return true
}

func (c *meilisearchClient) Driver() string {
	return "meilisearch"
}

func (c *meilisearchClient) Ping(ctx context.Context) error {
	resp, err := c.manager.HealthWithContext(ctx)
	if err != nil {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.Ping").Wrap(err)
	}
	if resp == nil || resp.Status != "available" {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.Ping").Errorf("meilisearch status is not available")
	}
	return nil
}

func (c *meilisearchClient) DeleteIndex(ctx context.Context, name string) error {
	prefixed := c.prefix + name
	_, err := c.manager.DeleteIndexWithContext(ctx, prefixed)
	if err != nil {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.DeleteIndex").Wrap(err)
	}
	return nil
}

func (c *meilisearchClient) Raw() any {
	return c.manager
}

type meilisearchIndex struct {
	name         string
	prefixedName string
	manager      meilisearch.IndexManager
}

func (idx *meilisearchIndex) Name() string {
	return idx.name
}

func (idx *meilisearchIndex) PrefixedName() string {
	return idx.prefixedName
}

func (idx *meilisearchIndex) Raw() any {
	return idx.manager
}

func (idx *meilisearchIndex) AddDocuments(ctx context.Context, documents any, primaryKey ...string) error {
	var opts *meilisearch.DocumentOptions
	if len(primaryKey) > 0 && primaryKey[0] != "" {
		pk := primaryKey[0]
		opts = &meilisearch.DocumentOptions{PrimaryKey: &pk}
	}

	_, err := idx.manager.AddDocumentsWithContext(ctx, documents, opts)
	if err != nil {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.AddDocuments").Wrap(err)
	}
	return nil
}

func (idx *meilisearchIndex) DeleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := idx.manager.DeleteDocumentsWithContext(ctx, ids, nil)
	if err != nil {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.DeleteDocuments").Wrap(err)
	}
	return nil
}

func (idx *meilisearchIndex) DeleteAllDocuments(ctx context.Context) error {
	_, err := idx.manager.DeleteAllDocumentsWithContext(ctx, nil)
	if err != nil {
		return oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.DeleteAllDocuments").Wrap(err)
	}
	return nil
}

func (idx *meilisearchIndex) Search(ctx context.Context, query string, opts ...SearchOption) (*SearchResult, error) {
	req := &SearchRequest{}
	for _, opt := range opts {
		opt(req)
	}

	meiliReq := &meilisearch.SearchRequest{
		Limit:  req.Limit,
		Offset: req.Offset,
		Filter: req.Filter,
		Sort:   req.Sort,
	}

	resp, err := idx.manager.SearchWithContext(ctx, query, meiliReq)
	if err != nil {
		return nil, oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.Search").Wrap(err)
	}

	var hits []map[string]any
	if err := resp.Hits.DecodeInto(&hits); err != nil {
		// 解码失败必须上抛：静默降级为空结果会让调用方无法区分"没有命中"与"数据损坏"。
		return nil, oops.Code(string(errs.CodeInternal)).In("search.Meilisearch.Search.DecodeHits").Wrap(err)
	}

	total := resp.EstimatedTotalHits
	if total == 0 && resp.TotalHits > 0 {
		total = resp.TotalHits
	}

	return &SearchResult{
		Hits:               hits,
		EstimatedTotalHits: total,
		Limit:              resp.Limit,
		Offset:             resp.Offset,
		ProcessingTimeMs:   resp.ProcessingTimeMs,
	}, nil
}
