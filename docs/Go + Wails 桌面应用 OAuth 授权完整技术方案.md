# Go + Wails 桌面应用 OAuth 授权完整技术方案

> 本文档详细研究了在 Go + Wails 桌面应用架构下，如何对 GitHub、Vercel、Netlify、Cloudflare Pages 四大平台实现 OAuth / API Token 授权与部署操作。

---

## 目录

1. [Wails 中打开外部浏览器的方法](#1-wails-中打开外部浏览器的方法)
2. [桌面应用 OAuth 标准模式 (RFC 8252 + PKCE)](#2-桌面应用-oauth-标准模式)
3. [Go 语言启动本地 HTTP 服务器作为 OAuth 回调](#3-go-语言启动本地-http-服务器作为-oauth-回调)
4. [GitHub OAuth 完整方案](#4-github-oauth-完整方案)
5. [Vercel OAuth / API 方案](#5-vercel-oauth--api-方案)
6. [Netlify OAuth / API 方案](#6-netlify-oauth--api-方案)
7. [Cloudflare Pages API 方案](#7-cloudflare-pages-api-方案)
8. [统一架构设计建议](#8-统一架构设计建议)

---

## 1. Wails 中打开外部浏览器的方法

### 1.1 内置 API: `runtime.BrowserOpenURL`

Wails v2 提供了内置的运行时 API 用于在系统默认浏览器中打开 URL。这是发起 OAuth 授权流程的核心方法。

**Go 后端调用方式：**

```go
package main

import (
    "context"
    "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
    ctx context.Context
}

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
}

// StartOAuth 打开系统浏览器发起 OAuth 授权
func (a *App) StartOAuth(authURL string) {
    runtime.BrowserOpenURL(a.ctx, authURL)
}
```

**前端 (JavaScript/TypeScript) 调用方式：**

```typescript
import { BrowserOpenURL } from '../wailsjs/runtime/runtime';

function startOAuth() {
    const authURL = "https://github.com/login/oauth/authorize?client_id=xxx&redirect_uri=http://127.0.0.1:9876/callback&scope=repo&state=random_state";
    BrowserOpenURL(authURL);
}
```

### 1.2 使用说明

- `runtime.BrowserOpenURL(ctx, url)` 会调用操作系统默认浏览器打开指定 URL
- 在 Windows 上使用 `cmd /c start`，macOS 上使用 `open`，Linux 上使用 `xdg-open`
- 该方法是非阻塞的，调用后立即返回
- 完美适配 OAuth 流程：打开授权页面 -> 用户在浏览器中授权 -> 重定向回本地服务器

---

## 2. 桌面应用 OAuth 标准模式

### 2.1 RFC 8252 - OAuth 2.0 for Native Apps 要点

RFC 8252 定义了原生应用使用 OAuth 2.0 的最佳实践。核心要点如下：

**基本原则：**
- OAuth 2.0 授权请求应通过外部用户代理（主要是用户的浏览器）发起，而非嵌入式 WebView
- 原生应用应使用授权码流程（Authorization Code Flow）配合 PKCE，而非隐式流程（Implicit Flow）
- 禁止使用嵌入式用户代理（如 WebView），因为它们会给用户凭据带来安全风险

**回调 URI 的三种方式：**

| 方式 | 说明 | 推荐度 |
|------|------|--------|
| **Loopback Interface Redirect** | 使用 `http://127.0.0.1:{port}/callback` | **最推荐** |
| Private-Use URI Scheme | 使用自定义协议如 `myapp://callback` | 次选 |
| Claimed HTTPS Redirect | 使用平台特定的 HTTPS 重定向 | 复杂 |

**Loopback 重定向关键规则：**
- 使用 `http://127.0.0.1` (IPv4) 或 `http://[::1]` (IPv6)
- 授权服务器必须允许在请求时指定任意端口
- 使用 `http` 协议（非 `https`），因为是本地回环
- 应用从操作系统获取可用的临时端口

### 2.2 PKCE (Proof Key for Code Exchange)

PKCE 是 RFC 7636 定义的扩展，专门用于保护公共客户端（如桌面/移动应用）的授权码流程。

**工作原理：**

```
1. 客户端生成随机 code_verifier（43-128 字符的随机字符串）
2. 计算 code_challenge = BASE64URL(SHA256(code_verifier))
3. 授权请求中发送 code_challenge + code_challenge_method=S256
4. 令牌交换时发送原始 code_verifier
5. 授权服务器验证 SHA256(code_verifier) == code_challenge
```

**Go 实现 PKCE：**

```go
package oauth

import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
)

// GenerateCodeVerifier 生成 PKCE code_verifier
func GenerateCodeVerifier() (string, error) {
    b := make([]byte, 32) // 生成 32 字节的随机数据
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    // Base64 URL 编码，结果约 43 字符
    return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateCodeChallenge 根据 code_verifier 计算 code_challenge
func GenerateCodeChallenge(verifier string) string {
    h := sha256.Sum256([]byte(verifier))
    return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateState 生成防 CSRF 的 state 参数
func GenerateState() (string, error) {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return base64.RawURLEncoding.EncodeToString(b), nil
}
```

### 2.3 完整的桌面应用 OAuth 流程图

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│  Wails Desktop  │     │   Browser    │     │  OAuth Provider │
│      App        │     │              │     │  (e.g. GitHub)  │
└────────┬────────┘     └──────┬───────┘     └────────┬────────┘
         │                      │                      │
         │  1. 生成 state +     │                      │
         │     code_verifier    │                      │
         │                      │                      │
         │  2. 启动本地 HTTP    │                      │
         │     服务器 (随机端口) │                      │
         │                      │                      │
         │  3. BrowserOpenURL   │                      │
         │  ──────────────────> │  4. 打开授权页面      │
         │                      │  ──────────────────> │
         │                      │                      │
         │                      │  5. 用户登录并授权    │
         │                      │  <────────────────── │
         │                      │                      │
         │  6. 重定向到          │                      │
         │  127.0.0.1:port      │                      │
         │  /callback?code=xxx  │                      │
         │  <────────────────── │                      │
         │                      │                      │
         │  7. 用 code + code_verifier                 │
         │     交换 access_token │                      │
         │  ──────────────────────────────────────────>│
         │                      │                      │
         │  8. 返回 access_token│                      │
         │  <──────────────────────────────────────────│
         │                      │                      │
         │  9. 关闭本地 HTTP    │                      │
         │     服务器            │                      │
         │                      │                      │
```

---

## 3. Go 语言启动本地 HTTP 服务器作为 OAuth 回调

### 3.1 端口选择策略

| 策略 | 优点 | 缺点 |
|------|------|------|
| **随机端口 (`:0`)** | 不会端口冲突；符合 RFC 8252 | 需要 OAuth 注册时支持动态端口 |
| **固定端口** | 简单；兼容所有 OAuth 提供商 | 可能端口冲突 |
| **固定端口 + 备选** | 折中方案 | 实现稍复杂 |

**推荐策略：** 对于 GitHub OAuth（支持 localhost 任意端口），使用随机端口；对于不支持动态端口的提供商，使用固定端口（如 `9876`）。

### 3.2 完整的本地回调服务器实现

```go
package oauth

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "sync"
    "time"
)

// CallbackResult 保存 OAuth 回调结果
type CallbackResult struct {
    Code  string
    State string
    Error string
}

// LocalCallbackServer 本地 OAuth 回调服务器
type LocalCallbackServer struct {
    server   *http.Server
    listener net.Listener
    result   chan CallbackResult
    port     int
    mu       sync.Mutex
}

// NewLocalCallbackServer 创建新的回调服务器
// port 为 0 时使用系统分配的随机端口
func NewLocalCallbackServer(port int) (*LocalCallbackServer, error) {
    addr := fmt.Sprintf("127.0.0.1:%d", port)
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
    }

    actualPort := listener.Addr().(*net.TCPAddr).Port

    s := &LocalCallbackServer{
        listener: listener,
        result:   make(chan CallbackResult, 1),
        port:     actualPort,
    }

    mux := http.NewServeMux()
    mux.HandleFunc("/callback", s.handleCallback)

    s.server = &http.Server{
        Handler:      mux,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
    }

    return s, nil
}

// GetPort 返回实际监听端口
func (s *LocalCallbackServer) GetPort() int {
    return s.port
}

// GetRedirectURL 返回回调 URL
func (s *LocalCallbackServer) GetRedirectURL() string {
    return fmt.Sprintf("http://127.0.0.1:%d/callback", s.port)
}

// Start 启动服务器（非阻塞）
func (s *LocalCallbackServer) Start() {
    go func() {
        if err := s.server.Serve(s.listener); err != http.ErrServerClosed {
            fmt.Printf("HTTP server error: %v\n", err)
        }
    }()
}

// WaitForCallback 等待回调结果（带超时）
func (s *LocalCallbackServer) WaitForCallback(timeout time.Duration) (*CallbackResult, error) {
    select {
    case result := <-s.result:
        return &result, nil
    case <-time.After(timeout):
        return nil, fmt.Errorf("timeout waiting for OAuth callback")
    }
}

// Shutdown 关闭服务器
func (s *LocalCallbackServer) Shutdown() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return s.server.Shutdown(ctx)
}

// handleCallback 处理 OAuth 回调
func (s *LocalCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
    code := r.URL.Query().Get("code")
    state := r.URL.Query().Get("state")
    errMsg := r.URL.Query().Get("error")

    result := CallbackResult{
        Code:  code,
        State: state,
        Error: errMsg,
    }

    // 返回一个友好的 HTML 页面
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    if errMsg != "" {
        fmt.Fprintf(w, `<!DOCTYPE html><html><body>
            <h2>授权失败</h2>
            <p>错误信息: %s</p>
            <p>你可以关闭此窗口。</p>
        </body></html>`, errMsg)
    } else {
        fmt.Fprint(w, `<!DOCTYPE html><html><body>
            <h2>授权成功！</h2>
            <p>你可以关闭此浏览器标签页，返回应用程序。</p>
            <script>window.close();</script>
        </body></html>`)
    }

    // 发送结果到通道
    s.result <- result
}
```

### 3.3 使用示例

```go
// 1. 创建回调服务器（随机端口）
server, err := NewLocalCallbackServer(0)
if err != nil {
    log.Fatal(err)
}

// 2. 启动服务器
server.Start()
defer server.Shutdown()

// 3. 获取重定向 URL
redirectURL := server.GetRedirectURL()
// 例如: http://127.0.0.1:52341/callback

// 4. 构建授权 URL 并打开浏览器
authURL := fmt.Sprintf(
    "https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=repo&state=%s",
    clientID,
    url.QueryEscape(redirectURL),
    state,
)
runtime.BrowserOpenURL(ctx, authURL)

// 5. 等待回调（5 分钟超时）
result, err := server.WaitForCallback(5 * time.Minute)
if err != nil {
    log.Fatal("OAuth timeout:", err)
}

// 6. 验证 state 并用 code 交换 token
if result.State != expectedState {
    log.Fatal("state mismatch - possible CSRF attack")
}
// ... 用 result.Code 交换 access_token
```

---

## 4. GitHub OAuth 完整方案

### 4.1 GitHub OAuth App vs GitHub App

| 特性 | OAuth App | GitHub App |
|------|-----------|------------|
| **权限模型** | 粗粒度 scope（如 `repo`） | 细粒度权限（读/写分离） |
| **Token 过期** | 不过期（除非撤销） | 短期 token（1小时） |
| **仓库访问** | 用户所有可访问仓库 | 仅安装时选择的仓库 |
| **身份** | 以用户身份操作 | 可以以应用自身身份操作 |
| **Webhook** | 需单独配置 | 内置集中式 webhook |
| **速率限制** | 5000 次/小时（用户级） | 按安装数量和用户数量缩放 |
| **推荐场景** | 桌面应用中代用户操作 | 服务端自动化 |

**对于桌面应用的建议：使用 OAuth App**，因为它更简单且直接支持 Web Application Flow。

### 4.2 注册 GitHub OAuth App

1. 访问 GitHub Settings > Developer settings > OAuth Apps > New OAuth App
2. 填写以下信息：
   - **Application name**: 你的应用名称
   - **Homepage URL**: 应用主页
   - **Authorization callback URL**: `http://127.0.0.1/callback`
     - GitHub 允许 localhost 回调 URL
     - 注册时使用不带端口的 URL，实际运行时可以使用任意端口
   - **Enable Device Flow**: 可选，作为备选方案
3. 记录 `Client ID`，生成 `Client Secret`

> **注意**：对于桌面应用，Client Secret 无法真正保密。可以考虑不使用 Client Secret，而使用 PKCE 替代（GitHub 目前对 OAuth App 不完全支持 PKCE，但 GitHub App 的 user-to-server token 支持）。实践中，桌面应用通常仍然在代码中嵌入 Client Secret，这是公认的权衡。

### 4.3 GitHub OAuth Web Application Flow

**完整流程：**

#### Step 1: 重定向用户到 GitHub 授权页

```
GET https://github.com/login/oauth/authorize
```

参数：

| 参数 | 说明 |
|------|------|
| `client_id` | 必需。注册 OAuth App 时获得的 Client ID |
| `redirect_uri` | 回调 URL，如 `http://127.0.0.1:9876/callback` |
| `scope` | 权限范围，如 `repo user` |
| `state` | 随机字符串，防 CSRF |
| `allow_signup` | 是否允许未注册用户注册，默认 `true` |

#### Step 2: GitHub 重定向回你的应用

用户授权后，GitHub 重定向到：
```
http://127.0.0.1:9876/callback?code=AUTHORIZATION_CODE&state=YOUR_STATE
```

#### Step 3: 用授权码交换 Access Token

```
POST https://github.com/login/oauth/access_token
```

参数：
- `client_id`
- `client_secret`
- `code` (上一步获得的授权码)
- `redirect_uri`

响应（设置 `Accept: application/json` header）：
```json
{
    "access_token": "gho_xxxxxxxxxxxx",
    "token_type": "bearer",
    "scope": "repo,user"
}
```

### 4.4 Go 完整实现

```go
package github

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
)

const (
    AuthorizeURL    = "https://github.com/login/oauth/authorize"
    TokenURL        = "https://github.com/login/oauth/access_token"
    APIBase         = "https://api.github.com"
)

type GitHubOAuth struct {
    ClientID     string
    ClientSecret string
}

type TokenResponse struct {
    AccessToken string `json:"access_token"`
    TokenType   string `json:"token_type"`
    Scope       string `json:"scope"`
}

type GitHubUser struct {
    Login     string `json:"login"`
    ID        int64  `json:"id"`
    AvatarURL string `json:"avatar_url"`
    Name      string `json:"name"`
    Email     string `json:"email"`
}

// GetAuthURL 构建授权 URL
func (g *GitHubOAuth) GetAuthURL(redirectURI, state string, scopes []string) string {
    params := url.Values{
        "client_id":    {g.ClientID},
        "redirect_uri": {redirectURI},
        "state":        {state},
        "scope":        {joinScopes(scopes)},
    }
    return AuthorizeURL + "?" + params.Encode()
}

// ExchangeCode 用授权码交换 Access Token
func (g *GitHubOAuth) ExchangeCode(code, redirectURI string) (*TokenResponse, error) {
    data := url.Values{
        "client_id":     {g.ClientID},
        "client_secret": {g.ClientSecret},
        "code":          {code},
        "redirect_uri":  {redirectURI},
    }

    req, err := http.NewRequest("POST", TokenURL, bytes.NewBufferString(data.Encode()))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    req.Header.Set("Accept", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var tokenResp TokenResponse
    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return nil, err
    }
    return &tokenResp, nil
}

// GetUser 获取当前用户信息
// GET https://api.github.com/user
func (g *GitHubOAuth) GetUser(accessToken string) (*GitHubUser, error) {
    req, err := http.NewRequest("GET", APIBase+"/user", nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Accept", "application/vnd.github+json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var user GitHubUser
    if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
        return nil, err
    }
    return &user, nil
}

// CreateRepo 创建仓库
// POST https://api.github.com/user/repos
func CreateRepo(accessToken, name, description string, private bool) (map[string]interface{}, error) {
    body := map[string]interface{}{
        "name":        name,
        "description": description,
        "private":     private,
        "auto_init":   true, // 初始化 README
    }
    jsonBody, _ := json.Marshal(body)

    req, err := http.NewRequest("POST", APIBase+"/user/repos", bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    return result, nil
}

// EnableGitHubPages 启用 GitHub Pages
// POST https://api.github.com/repos/{owner}/{repo}/pages
func EnableGitHubPages(accessToken, owner, repo, branch, path string) error {
    body := map[string]interface{}{
        "source": map[string]string{
            "branch": branch, // 例如 "main"
            "path":   path,   // 例如 "/" 或 "/docs"
        },
    }
    jsonBody, _ := json.Marshal(body)

    apiURL := fmt.Sprintf("%s/repos/%s/%s/pages", APIBase, owner, repo)
    req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("failed to enable Pages: %s", string(bodyBytes))
    }
    return nil
}

// PushFileToRepo 通过 API 推送文件到仓库
// PUT https://api.github.com/repos/{owner}/{repo}/contents/{path}
func PushFileToRepo(accessToken, owner, repo, filePath, content, message string) error {
    body := map[string]interface{}{
        "message": message,
        "content": content, // Base64 编码的文件内容
    }
    jsonBody, _ := json.Marshal(body)

    apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", APIBase, owner, repo, filePath)
    req, err := http.NewRequest("PUT", apiURL, bytes.NewBuffer(jsonBody))
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("failed to push file: %s", string(bodyBytes))
    }
    return nil
}

func joinScopes(scopes []string) string {
    result := ""
    for i, s := range scopes {
        if i > 0 {
            result += " "
        }
        result += s
    }
    return result
}
```

### 4.5 GitHub 常用 Scope

| Scope | 说明 |
|-------|------|
| `repo` | 完整仓库访问（读写代码、commit、PR、issues） |
| `repo:status` | 读取 commit 状态 |
| `public_repo` | 仅公开仓库访问 |
| `user` | 读取用户资料信息 |
| `user:email` | 读取用户邮箱 |
| `read:org` | 读取组织信息 |

### 4.6 GitHub Device Flow（备选方案）

对于无法打开浏览器的环境，GitHub 还支持 Device Flow：

```go
// Step 1: 请求设备码
// POST https://github.com/login/device/code
// 参数: client_id, scope

// Step 2: 显示验证码给用户
// 返回: device_code, user_code, verification_uri, interval

// Step 3: 用户访问 https://github.com/login/device 输入 user_code

// Step 4: 轮询获取 token
// POST https://github.com/login/oauth/access_token
// 参数: client_id, device_code, grant_type=urn:ietf:params:oauth:grant-type:device_code
```

---

## 5. Vercel OAuth / API 方案

### 5.1 认证方式概述

Vercel 提供两种认证方式：

1. **Personal Access Token (推荐用于桌面应用)** - 用户手动在 Vercel 控制面板生成 Token
2. **Sign in with Vercel (OAuth 2.0)** - Vercel 的 OAuth 身份提供者，使用 OAuth 2.0 + OIDC

Vercel 的 IdP 使用 OAuth 2.0 授权框架。它支持 OpenID Connect (OIDC)，是一个构建在 OAuth 2.0 之上的认证层。

### 5.2 Vercel Access Token 方式（推荐）

这是桌面应用最简单的集成方式：

1. 用户登录 Vercel Dashboard
2. 前往 Settings > Tokens
3. 创建新的 Access Token
4. 将 Token 粘贴到桌面应用中

Vercel REST API 的所有端点位于 `https://api.vercel.com` 下，所有请求需要 Access Token 进行认证。

### 5.3 Sign in with Vercel (OAuth 2.0)

如果需要更流畅的用户体验，可以使用 Vercel 的 OAuth 2.0 流程。其流程基于标准 OAuth 2.0 授权框架，允许你的应用请求访问 Vercel 身份提供者 (IdP) 中的用户数据。

**流程：**

1. 在 Vercel 注册你的应用（获取 Client ID 和 Client Secret）
2. 配置 authorization callback URL
3. 用户首次登录时，Vercel 会显示一个同意页面来审核请求的权限
4. 标准 OAuth 2.0 授权码流程
5. 获取 Access Token

> 注意：客户端认证方式 `none` 适用于公共、未认证的非机密客户端，无需客户端认证 - 适合无法安全存储密钥的公共应用。对于单页应用(SPA)、移动应用和 CLI 来说，这是合适的客户端认证方式。

### 5.4 Vercel 部署 API

**核心 API 端点：**

#### 创建项目
```
POST https://api.vercel.com/v9/projects
Authorization: Bearer <TOKEN>
```

```go
func CreateVercelProject(token, name string) error {
    body := map[string]interface{}{
        "name":      name,
        "framework": nil, // 静态站点不需要框架
    }
    jsonBody, _ := json.Marshal(body)

    req, _ := http.NewRequest("POST", "https://api.vercel.com/v9/projects", bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    return nil
}
```

#### 上传文件
在创建部署之前，需要先上传所有文件。

```
POST https://api.vercel.com/v2/files
Authorization: Bearer <TOKEN>
Content-Type: application/octet-stream
x-vercel-digest: <SHA1_OF_FILE>
Content-Length: <FILE_SIZE>
```

#### 创建部署
```
POST https://api.vercel.com/v13/deployments
Authorization: Bearer <TOKEN>
```

```go
func DeployToVercel(token, projectName string, files []VercelFile) error {
    body := map[string]interface{}{
        "name": projectName,
        "files": files, // [{file: "path", sha: "xxx", size: 123}]
        "projectSettings": map[string]interface{}{
            "framework": nil,
        },
    }
    jsonBody, _ := json.Marshal(body)

    req, _ := http.NewRequest("POST", "https://api.vercel.com/v13/deployments", bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    // result["url"] 是部署后的 URL
    return nil
}

type VercelFile struct {
    File string `json:"file"` // 文件路径
    SHA  string `json:"sha"`  // SHA1 哈希
    Size int64  `json:"size"` // 文件大小
}
```

### 5.5 桌面应用推荐方案

**方案 A: Personal Access Token（最简单）**
- 引导用户到 Vercel 控制面板生成 Token
- 用户将 Token 粘贴到应用中
- 应用保存 Token 用于 API 调用

**方案 B: Sign in with Vercel OAuth**
- 使用标准 OAuth 2.0 流程
- 本地 HTTP 服务器作为回调
- 获取 Access Token 后调用 REST API

---

## 6. Netlify OAuth / API 方案

### 6.1 认证方式概述

Netlify 使用 OAuth2 进行认证，所有请求必须使用 HTTPS。提供两种认证方式：

1. **Personal Access Token (PAT)** - 用户手动生成，适合脚本和简单集成
2. **OAuth 2.0** - 完整的 OAuth 流程，适合公开集成

对于公开集成，Netlify 要求必须使用 OAuth2。这允许用户授权你的应用代表他们使用 Netlify，无需复制粘贴 API tokens 或接触敏感的登录信息。

### 6.2 注册 Netlify OAuth Application

1. 访问 https://app.netlify.com/applications
2. 在 OAuth Applications 部分，创建新应用
3. 获取 `Client ID` 和 `Client Secret`
4. 设置 Redirect URI（可使用 `http://127.0.0.1:{port}/callback`）

### 6.3 Netlify OAuth 流程

Netlify 使用 **Ticket-based OAuth** 流程，与标准 OAuth 稍有不同：

**关键端点：**
- OAuth2 授权端点: `https://app.netlify.com/authorize`
- API 基础 URL: `https://api.netlify.com/api/v1/`
- Ticket 相关端点:
  - `POST /oauth/tickets` - 创建 ticket
  - `GET /oauth/tickets/{ticket_id}` - 查看 ticket 状态
  - `POST /oauth/tickets/{ticket_id}/exchange` - 交换 token

**标准授权码流程：**

```
1. 将用户重定向到:
   https://app.netlify.com/authorize?
     response_type=token&
     client_id=YOUR_CLIENT_ID&
     redirect_uri=http://127.0.0.1:9876/callback&
     state=RANDOM_STATE

2. 用户授权后，Netlify 重定向回:
   http://127.0.0.1:9876/callback#access_token=TOKEN&token_type=bearer

3. 注意：Netlify 使用 fragment (#) 而不是 query string (?) 返回 token
```

### 6.4 Go 实现 Netlify OAuth

```go
package netlify

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
)

const (
    AuthorizeURL = "https://app.netlify.com/authorize"
    APIBase      = "https://api.netlify.com/api/v1"
)

type NetlifyOAuth struct {
    ClientID     string
    ClientSecret string
}

// GetAuthURL 构建 Netlify 授权 URL
func (n *NetlifyOAuth) GetAuthURL(redirectURI, state string) string {
    params := url.Values{
        "response_type": {"token"},
        "client_id":     {n.ClientID},
        "redirect_uri":  {redirectURI},
        "state":         {state},
    }
    return AuthorizeURL + "?" + params.Encode()
}
```

> **注意**：由于 Netlify 使用 fragment（`#access_token=...`）返回 token，本地 HTTP 服务器无法直接从 URL 中获取 fragment。解决方案是在回调页面中使用 JavaScript 解析 fragment 并通过表单 POST 或新请求发送给本地服务器。

**回调处理的特殊逻辑：**

```go
// handleCallback 需要两阶段处理
func (s *LocalCallbackServer) handleNetlifyCallback(w http.ResponseWriter, r *http.Request) {
    // 第一次请求：浏览器带着 fragment 访问回调
    // 服务端拿不到 fragment，所以返回一个带 JS 的页面
    if r.URL.Query().Get("access_token") == "" {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, `<!DOCTYPE html><html><body>
            <p>正在完成授权...</p>
            <script>
                // 从 URL fragment 中提取 token
                const hash = window.location.hash.substring(1);
                const params = new URLSearchParams(hash);
                const token = params.get('access_token');
                if (token) {
                    // 将 token 发送到本地服务器
                    fetch('/token?access_token=' + encodeURIComponent(token))
                        .then(() => {
                            document.body.innerHTML = '<h2>授权成功！你可以关闭此标签页。</h2>';
                            window.close();
                        });
                }
            </script>
        </body></html>`)
        return
    }
}

// handleToken 接收前端 JS 转发的 token
func (s *LocalCallbackServer) handleToken(w http.ResponseWriter, r *http.Request) {
    token := r.URL.Query().Get("access_token")
    s.result <- CallbackResult{Code: token}
    w.WriteHeader(http.StatusOK)
}
```

### 6.5 Netlify 部署 API

Netlify 支持两种部署方式：文件摘要方式和 ZIP 文件方式。

#### ZIP 文件部署（最简单）

```go
// DeployToNetlify 通过 ZIP 文件部署到 Netlify
func DeployToNetlify(token, siteID string, zipData []byte) error {
    apiURL := fmt.Sprintf("%s/sites/%s/deploys", APIBase, siteID)

    req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(zipData))
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/zip")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("deploy failed: %s", string(body))
    }
    return nil
}

// CreateNetlifySite 创建新站点
func CreateNetlifySite(token, name string) (map[string]interface{}, error) {
    body := map[string]interface{}{}
    if name != "" {
        body["name"] = name // 子域名：name.netlify.app
    }
    jsonBody, _ := json.Marshal(body)

    req, err := http.NewRequest("POST", APIBase+"/sites", bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    return result, nil
}

// CreateAndDeployNetlify 创建站点并同时部署
// 可以在一个请求中完成创建和部署
func CreateAndDeployNetlify(token string, zipData []byte) (map[string]interface{}, error) {
    req, err := http.NewRequest("POST", APIBase+"/sites", bytes.NewBuffer(zipData))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/zip")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    return result, nil
}
```

#### 文件摘要部署（增量部署，推荐大型站点）

```go
// FileDigestDeploy 使用文件摘要方式部署
func FileDigestDeploy(token, siteID string, files map[string]string) error {
    // Step 1: 创建 deploy，提交文件摘要
    // files: {"/index.html": "sha1hash", "/style.css": "sha1hash"}
    body := map[string]interface{}{
        "files": files,
    }
    jsonBody, _ := json.Marshal(body)

    apiURL := fmt.Sprintf("%s/sites/%s/deploys", APIBase, siteID)
    req, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    var deployResult struct {
        ID       string   `json:"id"`
        Required []string `json:"required"` // 需要上传的文件 SHA1 列表
    }
    json.NewDecoder(resp.Body).Decode(&deployResult)

    // Step 2: 上传必需的文件
    // PUT /api/v1/deploys/{deploy_id}/files/{path}
    // Content-Type: application/octet-stream

    return nil
}
```

### 6.6 Netlify 速率限制

- 普通请求：每分钟 500 次
- 部署操作：每分钟 3 次，每天 100 次

---

## 7. Cloudflare Pages API 方案

### 7.1 认证方式

Cloudflare Pages **不支持 OAuth**，使用 **API Token** 进行认证。

**创建 API Token 的步骤：**
1. 登录 Cloudflare Dashboard
2. 前往 My Profile > API Tokens
3. 选择 Create Token
4. 选择模板或自定义权限
5. 设置权限组（Account、User 或 Zone），选择授予的访问级别
6. 选择 token 被授权访问的资源

**桌面应用集成方案：**
- 引导用户在 Cloudflare Dashboard 创建 API Token
- 需要 `Cloudflare Pages:Edit` 权限
- 用户将 Token 和 Account ID 粘贴到应用中

### 7.2 Cloudflare Pages 部署方式

Cloudflare Pages 支持 Direct Upload，允许将预构建资产上传到 Pages 并部署到 Cloudflare 全球网络。如果想集成自己的构建平台或从本地计算机上传，应选择 Direct Upload 而非 Git 集成。

**两种部署途径：**
1. **Wrangler CLI** - 官方命令行工具（推荐）
2. **REST API** - 直接使用 Cloudflare API（文档有限）

### 7.3 使用 Wrangler CLI 部署

Wrangler 是官方支持的方式，单个文件夹（不支持 ZIP 文件）：

```go
import "os/exec"

func DeployWithWrangler(accountID, apiToken, projectName, directory string) error {
    cmd := exec.Command("wrangler", "pages", "deploy", directory,
        "--project-name", projectName,
    )
    cmd.Env = append(os.Environ(),
        "CLOUDFLARE_ACCOUNT_ID="+accountID,
        "CLOUDFLARE_API_TOKEN="+apiToken,
    )
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
```

### 7.4 使用 REST API 部署

Cloudflare Pages 的 REST API 端点可以管理部署和构建以及配置项目。

**核心端点：**

```
# 创建项目
POST https://api.cloudflare.com/client/v4/accounts/{account_id}/pages/projects

# 获取项目列表
GET https://api.cloudflare.com/client/v4/accounts/{account_id}/pages/projects

# 创建部署
POST https://api.cloudflare.com/client/v4/accounts/{account_id}/pages/projects/{project_name}/deployments

# 获取部署列表
GET https://api.cloudflare.com/client/v4/accounts/{account_id}/pages/projects/{project_name}/deployments
```

**Go 实现：**

```go
package cloudflare

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
)

const CFAPIBase = "https://api.cloudflare.com/client/v4"

type CloudflarePages struct {
    AccountID string
    APIToken  string
}

// CreateProject 创建 Cloudflare Pages 项目
func (c *CloudflarePages) CreateProject(name string) error {
    body := map[string]interface{}{
        "name":              name,
        "production_branch": "main",
    }
    jsonBody, _ := json.Marshal(body)

    apiURL := fmt.Sprintf("%s/accounts/%s/pages/projects", CFAPIBase, c.AccountID)
    req, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+c.APIToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("create project failed: %s", string(bodyBytes))
    }
    return nil
}

// DeployFiles 通过 Direct Upload API 部署文件
// 使用 multipart/form-data 上传文件
func (c *CloudflarePages) DeployFiles(projectName, directory string) error {
    apiURL := fmt.Sprintf("%s/accounts/%s/pages/projects/%s/deployments",
        CFAPIBase, c.AccountID, projectName)

    // 创建 multipart form
    var buf bytes.Buffer
    writer := multipart.NewWriter(&buf)

    // 遍历目录，添加所有文件
    err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() {
            return err
        }

        relPath, _ := filepath.Rel(directory, path)
        part, err := writer.CreateFormFile(relPath, relPath)
        if err != nil {
            return err
        }

        file, err := os.Open(path)
        if err != nil {
            return err
        }
        defer file.Close()

        _, err = io.Copy(part, file)
        return err
    })
    if err != nil {
        return err
    }
    writer.Close()

    req, _ := http.NewRequest("POST", apiURL, &buf)
    req.Header.Set("Authorization", "Bearer "+c.APIToken)
    req.Header.Set("Content-Type", writer.FormDataContentType())

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("deploy failed: %s", string(bodyBytes))
    }
    return nil
}
```

### 7.5 限制

| 上传方式 | 文件数量限制 | 单文件大小 |
|---------|------------|-----------|
| Wrangler | 20,000 文件 | 25 MiB |
| 拖拽上传 | 1,000 文件 | 25 MiB |

---

## 8. 统一架构设计建议

### 8.1 各平台认证方式对比

| 平台 | OAuth 支持 | Token 方式 | 桌面应用推荐 |
|------|-----------|-----------|-------------|
| **GitHub** | 完整 OAuth 2.0 | Personal Access Token | **OAuth 授权码流程** |
| **Vercel** | Sign in with Vercel (OAuth 2.0) | Personal Access Token | **Personal Access Token** 或 OAuth |
| **Netlify** | OAuth 2.0（Ticket-based） | Personal Access Token | **OAuth** 或 PAT |
| **Cloudflare** | 不支持 OAuth | API Token | **API Token**（唯一选择） |

### 8.2 推荐的统一架构

```go
// AuthProvider 统一认证接口
type AuthProvider interface {
    // GetName 返回提供商名称
    GetName() string

    // NeedsOAuth 是否需要 OAuth 流程
    NeedsOAuth() bool

    // StartOAuth 开始 OAuth 流程，返回授权 URL
    StartOAuth(callbackURL string) (authURL string, state string, err error)

    // HandleCallback 处理回调，返回 Token
    HandleCallback(code, state string) (token string, err error)

    // SetToken 直接设置 Token（手动输入方式）
    SetToken(token string) error

    // ValidateToken 验证 Token 是否有效
    ValidateToken() error

    // GetUserInfo 获取用户信息
    GetUserInfo() (*UserInfo, error)
}

// Deployer 统一部署接口
type Deployer interface {
    // CreateProject 创建项目/站点
    CreateProject(name string) error

    // Deploy 部署文件
    Deploy(projectID string, files map[string][]byte) (*DeployResult, error)

    // GetDeployStatus 获取部署状态
    GetDeployStatus(deployID string) (string, error)
}
```

### 8.3 Token 存储

桌面应用需要安全存储 OAuth Token。推荐方案：

```go
package keyring

import (
    "github.com/zalando/go-keyring"
)

const ServiceName = "YourApp"

// SaveToken 使用系统密钥链存储 Token
func SaveToken(provider, token string) error {
    return keyring.Set(ServiceName, provider, token)
}

// GetToken 从系统密钥链获取 Token
func GetToken(provider string) (string, error) {
    return keyring.Get(ServiceName, provider)
}

// DeleteToken 从系统密钥链删除 Token
func DeleteToken(provider string) error {
    return keyring.Delete(ServiceName, provider)
}
```

这样 Token 会存储在：
- **macOS**: Keychain
- **Windows**: Credential Manager
- **Linux**: Secret Service (GNOME Keyring / KWallet)

### 8.4 完整的 OAuth 启动流程（在 Wails App 中）

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
    ctx            context.Context
    githubOAuth    *GitHubOAuth
    callbackServer *LocalCallbackServer
}

// LoginWithGitHub 发起 GitHub OAuth 登录
func (a *App) LoginWithGitHub() (*GitHubUser, error) {
    // 1. 创建本地回调服务器
    server, err := NewLocalCallbackServer(0) // 随机端口
    if err != nil {
        return nil, fmt.Errorf("failed to start callback server: %w", err)
    }
    server.Start()
    defer server.Shutdown()

    // 2. 生成 state
    state, _ := GenerateState()

    // 3. 构建授权 URL
    redirectURL := server.GetRedirectURL()
    authURL := a.githubOAuth.GetAuthURL(redirectURL, state, []string{"repo", "user"})

    // 4. 打开浏览器
    runtime.BrowserOpenURL(a.ctx, authURL)

    // 5. 等待回调
    result, err := server.WaitForCallback(5 * time.Minute)
    if err != nil {
        return nil, fmt.Errorf("OAuth callback timeout: %w", err)
    }

    // 6. 验证 state
    if result.State != state {
        return nil, fmt.Errorf("state mismatch")
    }

    // 7. 交换 token
    tokenResp, err := a.githubOAuth.ExchangeCode(result.Code, redirectURL)
    if err != nil {
        return nil, fmt.Errorf("token exchange failed: %w", err)
    }

    // 8. 保存 token
    SaveToken("github", tokenResp.AccessToken)

    // 9. 获取用户信息
    user, err := a.githubOAuth.GetUser(tokenResp.AccessToken)
    if err != nil {
        return nil, fmt.Errorf("failed to get user: %w", err)
    }

    return user, nil
}
```

---

## 附录 A: 关键 API 端点速查表

### GitHub API
| 操作 | 方法 | 端点 |
|------|------|------|
| 获取当前用户 | GET | `/user` |
| 创建仓库 | POST | `/user/repos` |
| 推送文件 | PUT | `/repos/{owner}/{repo}/contents/{path}` |
| 启用 Pages | POST | `/repos/{owner}/{repo}/pages` |
| 查看 Pages 状态 | GET | `/repos/{owner}/{repo}/pages` |

### Vercel API
| 操作 | 方法 | 端点 |
|------|------|------|
| 上传文件 | POST | `/v2/files` |
| 创建部署 | POST | `/v13/deployments` |
| 创建项目 | POST | `/v9/projects` |
| 获取部署状态 | GET | `/v13/deployments/{id}` |

### Netlify API
| 操作 | 方法 | 端点 |
|------|------|------|
| 创建站点 | POST | `/api/v1/sites` |
| ZIP 部署 | POST | `/api/v1/sites/{site_id}/deploys` (Content-Type: application/zip) |
| 文件摘要部署 | POST | `/api/v1/sites/{site_id}/deploys` (Content-Type: application/json) |
| 获取部署状态 | GET | `/api/v1/deploys/{deploy_id}` |

### Cloudflare Pages API
| 操作 | 方法 | 端点 |
|------|------|------|
| 创建项目 | POST | `/client/v4/accounts/{account_id}/pages/projects` |
| 创建部署 | POST | `/client/v4/accounts/{account_id}/pages/projects/{name}/deployments` |
| 获取部署列表 | GET | `/client/v4/accounts/{account_id}/pages/projects/{name}/deployments` |

---

## 附录 B: 安全注意事项

1. **Client Secret**：桌面应用中 Client Secret 无法真正保密，应尽量使用 PKCE
2. **Token 存储**：使用操作系统密钥链（Keychain/Credential Manager），不要明文存储
3. **State 参数**：必须验证 state 参数以防止 CSRF 攻击
4. **HTTPS**：与 OAuth 服务器通信时始终使用 HTTPS
5. **最小权限**：申请最小必需的 scope/权限
6. **Token 过期**：实现 Token 刷新逻辑（如果提供商支持 refresh token）

---

*文档生成日期：2026-02-25*
*研究涵盖：Wails v2, GitHub OAuth, Vercel API, Netlify API, Cloudflare Pages API*
