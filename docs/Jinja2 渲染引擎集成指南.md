# Jinja2 渲染引擎集成指南

## 1. 安装依赖

```bash
cd backend
go get github.com/flosch/pongo2/v6
```

## 2. 新增文件

将 `jinja2_renderer.go` 放到 `backend/internal/render/` 目录下，与现有的 `ejs_renderer.go` 和 `go_renderer.go` 同级：

```
backend/internal/render/
├── renderer.go           ← 接口定义（需更新注释）
├── ejs_renderer.go       ← EJS 渲染器（已有）
├── go_renderer.go        ← Go Template 渲染器（已有）
└── jinja2_renderer.go    ← Jinja2 渲染器（新增）
```

## 3. 更新 renderer.go 接口注释

```go
// GetEngineType 获取引擎类型
// 返回: "gotemplate" 或 "ejs" 或 "jinja2"
GetEngineType() string
```

## 4. 更新工厂方法

在你项目中创建渲染器的地方（可能在 render_service.go 或 facade 层），添加 jinja2 分支：

```go
func NewRenderer(engineType string, config RenderConfig) (ThemeRenderer, error) {
    switch engineType {
    case "ejs":
        return NewEjsRenderer(config)
    case "gotemplate":
        return NewGoTemplateRenderer(config), nil
    case "jinja2":
        return NewJinja2Renderer(config), nil
    default:
        return nil, fmt.Errorf("不支持的模板引擎: %s", engineType)
    }
}
```

## 5. 主题配置中声明引擎类型

在主题的 `config.json` 或 `theme.toml` 中添加引擎类型字段：

```json
{
  "name": "my-jinja2-theme",
  "engine": "jinja2",
  "version": "1.0.0"
}
```

## 6. Jinja2 主题模板示例

### base.html（根布局）

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{% block title %}{{ config.siteName | default:"我的博客" }}{% endblock %}</title>
    <meta name="description" content="{% block description %}{{ config.description | default:"" }}{% endblock %}">
    {% block head %}{% endblock %}
    <link rel="stylesheet" href="{{ config.baseUrl }}/styles/main.css">
</head>
<body>
    {% include "partials/header.html" %}

    <main class="container">
        {% block content %}{% endblock %}
    </main>

    {% include "partials/footer.html" %}

    {% block scripts %}{% endblock %}
</body>
</html>
```

### index.html（首页）

```html
{% extends "base.html" %}

{% block title %}{{ config.siteName }}{% endblock %}

{% block content %}
<div class="post-list">
    {% for post in posts %}
    <article class="post-card">
        {% if post.feature %}
        <img class="post-cover" src="{{ post.feature }}" alt="{{ post.title }}">
        {% endif %}

        <h2 class="post-title">
            <a href="{{ post.link }}">{{ post.title }}</a>
        </h2>

        <div class="post-meta">
            <time>{{ post.dateFormat }}</time>
            <span>{{ post.content | reading_time }}</span>
        </div>

        <p class="post-excerpt">{{ post.content | excerpt }}</p>

        <div class="post-tags">
            {% for tag in post.tags %}
            <a href="{{ tag.link }}" class="tag">{{ tag.name }}</a>
            {% endfor %}
        </div>
    </article>
    {% empty %}
    <p class="no-posts">还没有文章，开始写作吧！</p>
    {% endfor %}
</div>

{% if pagination %}
<nav class="pagination">
    {% if pagination.prev %}
    <a href="{{ pagination.prev }}" class="prev">上一页</a>
    {% endif %}
    <span>{{ pagination.current }} / {{ pagination.total }}</span>
    {% if pagination.next %}
    <a href="{{ pagination.next }}" class="next">下一页</a>
    {% endif %}
</nav>
{% endif %}
{% endblock %}
```

### post.html（文章页）

```html
{% extends "base.html" %}

{% block title %}{{ post.title }} - {{ config.siteName }}{% endblock %}

{% block description %}{{ post.content | excerpt:160 }}{% endblock %}

{% block head %}
<meta name="keywords" content="{{ post.tags | join:', ' }}">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/KaTeX/0.10.0/katex.min.css">
{% endblock %}

{% block content %}
<article class="post-detail">
    <header class="post-header">
        <h1 class="post-title">{{ post.title }}</h1>
        <div class="post-info">
            <time>{{ post.dateFormat }}</time>
            <span>{{ post.content | reading_time }}</span>
            {% for tag in post.tags %}
            <a href="{{ tag.link }}" class="post-tag"># {{ tag.name }}</a>
            {% endfor %}
        </div>
    </header>

    {% if theme_config.showFeatureImage and post.feature %}
    <img class="post-feature-image" src="{{ post.feature }}" alt="{{ post.title }}">
    {% endif %}

    <div class="post-content-wrapper">
        <div class="post-content">{{ post.content | safe }}</div>
        {% if post.toc %}
        <aside class="toc-container">{{ post.toc | safe }}</aside>
        {% endif %}
    </div>
</article>

{% if post.nextPost and not post.hideInList %}
<nav class="next-post">
    <span class="label">下一篇</span>
    <a href="{{ post.nextPost.link }}">
        <h3>{{ post.nextPost.title }}</h3>
    </a>
</nav>
{% endif %}

{% if theme_config.showComment %}
{% include "partials/comments/" ~ theme_config.commentPlatform ~ ".html" %}
{% endif %}
{% endblock %}

{% block scripts %}
<script src="//cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.5.1/build/highlight.min.js"></script>
<script>hljs.highlightAll();</script>
{% endblock %}
```

### partials/header.html

```html
<header class="site-header">
    <div class="header-inner">
        <a href="{{ config.baseUrl }}" class="site-title">
            {% if config.avatar %}
            <img src="{{ config.avatar }}" alt="" class="avatar">
            {% endif %}
            {{ config.siteName }}
        </a>
        <nav class="site-nav">
            {% for menu in menus %}
            <a href="{{ menu.link }}"
               {% if menu.openType == "external" %}target="_blank" rel="noopener"{% endif %}>
                {{ menu.name }}
            </a>
            {% endfor %}
        </nav>
    </div>
</header>
```

### partials/footer.html

```html
<footer class="site-footer">
    <p>
        Powered by <a href="https://gridea.pro" target="_blank">Gridea Pro</a>
        {% if config.footerInfo %}
        | {{ config.footerInfo | safe }}
        {% endif %}
    </p>
</footer>
```

## 7. 三种引擎语法对照速查

| 功能 | EJS | Go Templates | Jinja2 (Pongo2) |
|------|-----|-------------|-----------------|
| 输出变量 | `<%= post.title %>` | `{{.Post.Title}}` | `{{ post.title }}` |
| 原始 HTML | `<%- content %>` | `{{.Content}}` | `{{ content \| safe }}` |
| 条件 | `<% if (x) { %>` | `{{if .X}}` | `{% if x %}` |
| 否则 | `<% } else { %>` | `{{else}}` | `{% else %}` |
| 结束 | `<% } %>` | `{{end}}` | `{% endif %}` |
| 循环 | `<% posts.forEach(p => { %>` | `{{range .Posts}}` | `{% for p in posts %}` |
| 空列表 | 需手动判断 | 不支持 | `{% empty %}` |
| 引入 | `<%- include('./x') %>` | `{{template "x" .}}` | `{% include "x.html" %}` |
| 继承 | 不支持 | 不支持 | `{% extends "base.html" %}` |
| 块定义 | 不支持 | 不支持 | `{% block name %}{% endblock %}` |
| 过滤器 | JS 方法链 | 管道函数 | `{{ val \| filter }}` |
| 注释 | `<%# 注释 %>` | `{{/* 注释 */}}` | `{# 注释 #}` |

## 8. 自定义 Filter 速查

Gridea Pro 注册的专属 filter：

| Filter | 用法 | 输出示例 |
|--------|------|---------|
| `reading_time` | `{{ post.content \| reading_time }}` | `3 min read` |
| `excerpt` | `{{ post.content \| excerpt }}` | 前 200 字... |
| `excerpt:300` | `{{ post.content \| excerpt:300 }}` | 前 300 字... |
| `word_count` | `{{ post.content \| word_count }}` | `1234` |
| `strip_html` | `{{ content \| strip_html }}` | 纯文本 |
| `relative` | `{{ post.date \| relative }}` | `3 天前` |
| `timeago` | `{{ post.date \| timeago }}` | `3 天前`（别名）|
| `to_json` | `{{ site \| to_json }}` | JSON 字符串 |
| `group_by` | `{{ posts \| group_by:"year" }}` | 按年分组 |

Pongo2 内置的常用 filter：

| Filter | 用法 | 说明 |
|--------|------|------|
| `safe` | `{{ html \| safe }}` | 不转义 HTML |
| `date` | `{{ time \| date:"2006-01-02" }}` | 格式化日期 |
| `default` | `{{ val \| default:"fallback" }}` | 默认值 |
| `length` | `{{ list \| length }}` | 长度 |
| `join` | `{{ list \| join:", " }}` | 连接 |
| `truncatechars` | `{{ text \| truncatechars:100 }}` | 截断字符 |
| `upper` / `lower` | `{{ text \| upper }}` | 大小写 |
| `title` | `{{ text \| title }}` | 首字母大写 |
| `slugify` | `{{ text \| slugify }}` | URL 友好化 |
| `striptags` | `{{ html \| striptags }}` | 移除标签 |
| `first` / `last` | `{{ list \| first }}` | 首/末元素 |
| `slice` | `{{ list \| slice:"0:5" }}` | 切片 |
| `urlencode` | `{{ url \| urlencode }}` | URL 编码 |
| `wordcount` | `{{ text \| wordcount }}` | 英文词数 |
