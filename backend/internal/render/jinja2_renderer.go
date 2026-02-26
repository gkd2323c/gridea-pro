package render

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gridea-pro/backend/internal/template"

	pongo2 "github.com/flosch/pongo2/v6"
)

// 依赖说明:
// go get github.com/flosch/pongo2/v6
//
// Jinja2 主题目录结构:
//
//	themes/{themeName}/templates/
//	├── base.html              ← 根布局（定义 block 占位符）
//	├── index.html             ← 首页（{% extends "base.html" %}）
//	├── post.html              ← 文章页
//	├── archive.html           ← 归档页
//	├── tag.html               ← 标签页
//	├── tags.html              ← 标签列表
//	└── partials/              ← 可复用组件
//	    ├── header.html        ← {% include "partials/header.html" %}
//	    ├── footer.html
//	    └── comments.html

// registerOnce 确保自定义 filter 只注册一次（pongo2 filter 是全局的）
var registerOnce sync.Once

// Jinja2Renderer Jinja2 渲染器
// 使用 Pongo2（Go 实现的 Django/Jinja2 模板引擎）
// 支持模板继承(extends/block)、include、filter 管道等 Jinja2 核心特性
type Jinja2Renderer struct {
	config RenderConfig

	// Pongo2 模板集，管理模板加载和命名空间
	templateSet *pongo2.TemplateSet

	// 模板缓存
	cache     map[string]*pongo2.Template
	cacheLock sync.RWMutex
}

// NewJinja2Renderer 创建 Jinja2 渲染器
func NewJinja2Renderer(config RenderConfig) *Jinja2Renderer {
	// 注册自定义 filter（全局仅一次）
	registerOnce.Do(registerCustomFilters)

	themePath := filepath.Join(config.AppDir, "themes", config.ThemeName)
	templatesDir := filepath.Join(themePath, "templates")

	// 确保模板目录存在
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "WARN: templatesDir does not exist! %s\n", templatesDir)
		_ = os.MkdirAll(templatesDir, 0755)
	}

	fmt.Fprintf(os.Stderr, "INFO: Using Jinja2 templatesDir: %s\n", templatesDir)

	// 创建自定义清理加载器
	// SanitizingLoader 在读取模板文件后自动清理 {{ }}/{% %}/{# #} 标签内的换行符
	// 解决 Pongo2 Lexer 严格禁止标签内换行的兼容性问题
	var loader pongo2.TemplateLoader
	if sanitizingLoader, err := NewSanitizingLoader(templatesDir); err != nil {
		// Fallback：如果路径不存在，用标准加载器（后续 Render 时会报错）
		fmt.Fprintf(os.Stderr, "Warn: 创建模板加载器失败: %v\n", err)
		loader = pongo2.MustNewLocalFileSystemLoader(".")
	} else {
		loader = sanitizingLoader
	}

	// 创建模板集
	set := pongo2.NewSet("gridea", loader)
	set.Debug = false // 生产模式：启用内部缓存

	return &Jinja2Renderer{
		config:      config,
		templateSet: set,
		cache:       make(map[string]*pongo2.Template),
	}
}

// ============================================================
// ThemeRenderer 接口实现
// ============================================================

// Render 渲染指定模板
func (r *Jinja2Renderer) Render(templateName string, data *template.TemplateData) (string, error) {
	// 1. 获取编译后的模板
	tmpl, err := r.getTemplate(templateName)
	if err != nil {
		return "", fmt.Errorf("获取 Jinja2 模板失败: %w", err)
	}

	// 2. 构建模板上下文
	ctx := r.buildContext(data)

	// 3. 执行渲染
	result, err := tmpl.Execute(ctx)
	if err != nil {
		return "", fmt.Errorf("渲染 Jinja2 模板失败 [%s]: %w", templateName, err)
	}

	return result, nil
}

// GetEngineType 获取引擎类型标识
func (r *Jinja2Renderer) GetEngineType() string {
	return "jinja2"
}

// ClearCache 清除所有模板缓存
// 用于开发模式下主题文件变更后的热重载
func (r *Jinja2Renderer) ClearCache() {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

	// 清除自有缓存
	r.cache = make(map[string]*pongo2.Template)

	// 重建 TemplateSet 以清除 pongo2 内部缓存
	themePath := filepath.Join(r.config.AppDir, "themes", r.config.ThemeName)
	templatesDir := filepath.Join(themePath, "templates")
	var loader pongo2.TemplateLoader
	if sanitizingLoader, err := NewSanitizingLoader(templatesDir); err != nil {
		return
	} else {
		loader = sanitizingLoader
	}
	r.templateSet = pongo2.NewSet("gridea", loader)
	r.templateSet.Debug = false
}

// ============================================================
// 模板加载与缓存
// ============================================================

// getTemplate 获取编译后的模板，优先从缓存读取
func (r *Jinja2Renderer) getTemplate(name string) (*pongo2.Template, error) {
	// 检查缓存
	r.cacheLock.RLock()
	if tmpl, ok := r.cache[name]; ok {
		r.cacheLock.RUnlock()
		return tmpl, nil
	}
	r.cacheLock.RUnlock()

	// 按优先级尝试不同扩展名
	// .html 是最通用的，.jinja2 和 .j2 是 Jinja2 惯用扩展名
	extensions := []string{".html", ".jinja2", ".j2"}
	var tmpl *pongo2.Template
	var lastErr error

	for _, ext := range extensions {
		filename := name + ext
		tmpl, lastErr = r.templateSet.FromFile(filename)
		if lastErr == nil {
			break
		}

		// 打印实际的详细解析错误！这能帮助我们发现具体是哪里语法错了
		fmt.Fprintf(os.Stderr, "INFO: 尝试加载 %s 时发生错误: %v\n", filename, lastErr)
	}

	if tmpl == nil {
		return nil, fmt.Errorf("模板文件未成功加载 %s: 最后错误: %w", name, lastErr)
	}

	// 存入缓存
	r.cacheLock.Lock()
	r.cache[name] = tmpl
	r.cacheLock.Unlock()

	return tmpl, nil
}

// ============================================================
// 数据上下文构建
// ============================================================

// buildContext 将 TemplateData 转换为 pongo2.Context
// 通过 JSON 序列化/反序列化实现字段名从 PascalCase 到 snake_case/camelCase 的映射
// 使模板中可以使用 {{ post.title }} 而非 {{ post.Title }}
func (r *Jinja2Renderer) buildContext(data *template.TemplateData) pongo2.Context {
	if data == nil {
		return pongo2.Context{}
	}

	// 数据清洗：确保 nil slice 初始化为空 slice
	r.sanitizeData(data)

	ctx := pongo2.Context{
		// 站点配置
		"site":         toContextValue(data.Site),
		"config":       toContextValue(data.Site), // alias，方便主题开发者
		"theme_config": toContextValue(data.ThemeConfig),

		// 内容数据
		"post":  toContextValue(data.Post),
		"posts": toContextValue(data.Posts),
		"tags":  toContextValue(data.Tags),
		"menus": toContextValue(data.Menus),
		"memos": toContextValue(data.Memos),

		// 分页
		"pagination": toContextValue(data.Pagination),

		// 上下文信息
		"current_tag": toContextValue(data.Tag),

		// 实用工具
		"now": time.Now(),
	}

	// 兼容性：添加 site.posts / site.tags / site.menus 引用
	// 某些主题习惯从 site 对象访问全局数据
	if siteMap, ok := ctx["site"].(map[string]interface{}); ok {
		if _, exists := siteMap["posts"]; !exists {
			siteMap["posts"] = ctx["posts"]
		}
		if _, exists := siteMap["tags"]; !exists {
			siteMap["tags"] = ctx["tags"]
		}
		if _, exists := siteMap["menus"]; !exists {
			siteMap["menus"] = ctx["menus"]
		}
	}

	return ctx
}

// toContextValue 将 Go 结构体转换为 pongo2 友好的 map/slice
// 利用 JSON tag 实现字段名映射（PascalCase → camelCase/snake_case）
func toContextValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return v
	}
	return result
}

// sanitizeData 确保 nil slice 初始化为空 slice，避免模板遍历时报错
func (r *Jinja2Renderer) sanitizeData(data *template.TemplateData) {
	if data.Menus == nil {
		data.Menus = []template.MenuView{}
	}
	if data.Posts == nil {
		data.Posts = []template.PostView{}
	} else {
		for i := range data.Posts {
			if data.Posts[i].Tags == nil {
				data.Posts[i].Tags = []template.TagView{}
			}
			if data.Posts[i].Categories == nil {
				data.Posts[i].Categories = []template.CategoryView{}
			}
		}
	}
	if data.Tags == nil {
		data.Tags = []template.TagView{}
	}
	if data.Memos == nil {
		data.Memos = []template.MemoView{}
	}
	if data.Post.Tags == nil {
		data.Post.Tags = []template.TagView{}
	}
	if data.Post.Categories == nil {
		data.Post.Categories = []template.CategoryView{}
	}
}

// ============================================================
// 自定义 Filter 注册
// ============================================================

// registerCustomFilters 注册 Gridea Pro 专属的模板 filter
// 这些 filter 为博客场景提供便捷功能，是相对 EJS/Go Templates 的体验优势
func registerCustomFilters() {

	// ---- 内容处理类 ----

	// reading_time: 估算阅读时间（支持中文，按 400 字/分钟计算）
	// 用法: {{ post.content | reading_time }} → "3 min read"
	pongo2.RegisterFilter("reading_time", filterReadingTime)

	// excerpt: 截取文章摘要
	// 用法: {{ post.content | excerpt }} → 前 200 字
	//       {{ post.content | excerpt:300 }} → 前 300 字
	pongo2.RegisterFilter("excerpt", filterExcerpt)

	// word_count: 统计字数（CJK 字符感知）
	// 用法: {{ post.content | word_count }} → 1234
	pongo2.RegisterFilter("word_count", filterWordCount)

	// strip_html: 移除 HTML 标签（pongo2 内置 striptags 的中文友好版）
	// 用法: {{ post.content | strip_html }}
	pongo2.RegisterFilter("strip_html", filterStripHTML)

	// ---- 时间处理类 ----

	// relative: 相对时间显示
	// 用法: {{ post.date | relative }} → "3 天前"
	pongo2.RegisterFilter("relative", filterRelativeTime)

	// timeago: relative 的别名，兼容常见命名习惯
	pongo2.RegisterFilter("timeago", filterRelativeTime)

	// ---- 博客专用类 ----

	// json: 将值序列化为 JSON 字符串（用于 JSON-LD 结构化数据等场景）
	// 用法: {{ site | json }} → JSON 字符串
	pongo2.RegisterFilter("to_json", filterToJSON)

	// group_by: 按属性分组（用于归档页面按年/月分组）
	// 注意: pongo2 不支持带参数名的 filter，使用字符串参数
	// 用法: {% for year, posts in posts | group_by:"year" %}
	pongo2.RegisterFilter("group_by", filterGroupBy)
}

// ---- Filter 实现 ----

func filterReadingTime(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	content := in.String()

	// 移除 HTML 标签后统计
	cleaned := stripHTMLTags(content)
	charCount := utf8.RuneCountInString(cleaned)

	// 中文按 400 字/分钟，英文按 200 词/分钟
	// 简单策略：统计字符数，按 400 计算（对中英混合内容足够准确）
	minutes := int(math.Ceil(float64(charCount) / 400.0))
	if minutes < 1 {
		minutes = 1
	}

	return pongo2.AsValue(fmt.Sprintf("%d min read", minutes)), nil
}

func filterExcerpt(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	content := in.String()

	// 默认截取 200 字符，支持自定义长度
	length := 200
	if !param.IsNil() && param.Integer() > 0 {
		length = param.Integer()
	}

	// 先移除 HTML 标签
	cleaned := stripHTMLTags(content)
	runes := []rune(cleaned)

	if len(runes) <= length {
		return pongo2.AsValue(cleaned), nil
	}

	return pongo2.AsValue(string(runes[:length]) + "..."), nil
}

func filterWordCount(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	content := in.String()
	cleaned := stripHTMLTags(content)
	count := utf8.RuneCountInString(strings.TrimSpace(cleaned))
	return pongo2.AsValue(count), nil
}

func filterStripHTML(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	return pongo2.AsValue(stripHTMLTags(in.String())), nil
}

func filterRelativeTime(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	var t time.Time

	switch v := in.Interface().(type) {
	case time.Time:
		t = v
	case string:
		// 尝试多种常见日期格式
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		var parsed bool
		for _, format := range formats {
			var err error
			t, err = time.Parse(format, v)
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			return pongo2.AsValue(v), nil
		}
	default:
		return in, nil
	}

	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return pongo2.AsValue("刚刚"), nil
	case diff < time.Hour:
		return pongo2.AsValue(fmt.Sprintf("%d 分钟前", int(diff.Minutes()))), nil
	case diff < 24*time.Hour:
		return pongo2.AsValue(fmt.Sprintf("%d 小时前", int(diff.Hours()))), nil
	case diff < 30*24*time.Hour:
		return pongo2.AsValue(fmt.Sprintf("%d 天前", int(diff.Hours()/24))), nil
	case diff < 365*24*time.Hour:
		return pongo2.AsValue(fmt.Sprintf("%d 个月前", int(diff.Hours()/24/30))), nil
	default:
		return pongo2.AsValue(fmt.Sprintf("%d 年前", int(diff.Hours()/24/365))), nil
	}
}

func filterToJSON(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	data, err := json.Marshal(in.Interface())
	if err != nil {
		return pongo2.AsValue("{}"), nil
	}
	return pongo2.AsValue(string(data)), nil
}

func filterGroupBy(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.CanSlice() {
		return in, nil
	}

	key := param.String()
	if key == "" {
		return in, nil
	}

	// 将输入转换为 []interface{} 进行分组
	groups := make(map[string][]interface{})
	var order []string // 保持分组顺序

	for i := 0; i < in.Len(); i++ {
		item := in.Index(i)
		// 尝试从 map 中获取分组键的值
		var groupKey string
		if itemMap, ok := item.Interface().(map[string]interface{}); ok {
			if val, exists := itemMap[key]; exists {
				groupKey = fmt.Sprintf("%v", val)
			}
		}
		if groupKey == "" {
			groupKey = "other"
		}
		if _, exists := groups[groupKey]; !exists {
			order = append(order, groupKey)
		}
		groups[groupKey] = append(groups[groupKey], item.Interface())
	}

	// 转换为有序的 slice of maps，便于模板遍历
	var result []map[string]interface{}
	for _, k := range order {
		result = append(result, map[string]interface{}{
			"group": k,
			"items": groups[k],
		})
	}

	return pongo2.AsValue(result), nil
}

// ============================================================
// 工具函数
// ============================================================

// stripHTMLTags 简单高效地移除 HTML 标签
func stripHTMLTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}

	return b.String()
}
