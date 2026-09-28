// Package response 实现统一 HTTP 成功响应与分页信封封装。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Meta 是分页列表响应的元信息。
type Meta struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

// Page 是分页列表的响应信封；Items 恒为数组，不会序列化成 null。
type Page[T any] struct {
	Items []T  `json:"items"`
	Meta  Meta `json:"meta"`
}

// NewMeta 计算分页元信息，totalPages 向上取整。
func NewMeta(page, pageSize, total int) Meta {
	var totalPages int
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return Meta{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}
}

// OK 返回 200 与业务对象本身，不添加任何外层包装。
func OK[T any](c *gin.Context, data T) {
	c.JSON(http.StatusOK, data)
}

// Created 返回 201 与新建对象。
func Created[T any](c *gin.Context, data T) {
	c.JSON(http.StatusCreated, data)
}

// NoContent 返回 204，响应体为 0 字节。
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Paginated 返回分页信封，空列表同样保持结构完整。
func Paginated[T any](c *gin.Context, items []T, meta Meta) {
	if items == nil {
		items = []T{}
	}
	c.JSON(http.StatusOK, Page[T]{Items: items, Meta: meta})
}

// Collection 返回无信封的数组集合，确保 nil 切片恒定序列化为 [] 而非 null。
func Collection[T any](c *gin.Context, items []T) {
	if items == nil {
		items = []T{}
	}
	c.JSON(http.StatusOK, items)
}
