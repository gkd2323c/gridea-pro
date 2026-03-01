package service

import (
"encoding/json"
"fmt"
"os"
"path/filepath"

"github.com/microcosm-cc/bluemonday"
"gridea-pro/backend/internal/template"
)

// ─── 搜索数据 JSON ────────────────────────────────────────────────────────────

// renderSearchJSON 生成搜索数据 /api/search.json
// 包含所有已发布文章的标题、链接、日期和纯文本内容，供客户端搜索使用
func (s *RendererService) renderSearchJSON(buildDir string, data *template.TemplateData) error {
	var entries []searchEntry
	for _, post := range data.Posts {
		if post.HideInList {
			continue
		}
		// 将 HTML 内容转为纯文本用于搜索
		plainContent := stripHTMLForSearch(string(post.Content))
		// 限制内容长度（搜索不需要全文，5000 字足够）
		if len([]rune(plainContent)) > 5000 {
			plainContent = string([]rune(plainContent)[:5000])
		}
		entries = append(entries, searchEntry{
Title:   post.Title,
Link:    post.Link,
Date:    post.DateFormat,
Content: plainContent,
})
	}

	jsonData, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("序列化搜索数据失败: %w", err)
	}

	apiDir := filepath.Join(buildDir, "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		return err
	}

	s.logger.Info(fmt.Sprintf("✅ 搜索数据生成成功 (%d 篇文章)", len(entries)))
	return os.WriteFile(filepath.Join(apiDir, "search.json"), jsonData, 0644)
}

// stripHTMLForSearch 移除 HTML 标签，返回纯文本（用于搜索索引）。
func stripHTMLForSearch(s string) string {
	p := bluemonday.StrictPolicy()
	return p.Sanitize(s)
}
