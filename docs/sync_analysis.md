# Gridea Pro 同步功能逻辑与问题分析

## 1. 现有同步功能现状与问题

目前在点击客户端界面上的「同步」按钮后会一直转圈，主要原因是**前端与后端的事件通信断裂**以及**后端同步逻辑尚未真正实现**。

### 1.1 前端死循环转圈原因
在 `frontend/src/layouts/MainLayout.vue` 中，点击同步按钮的逻辑如下：

```javascript
const publish = () => {
  publishLoading.value = true
  EventsEmit('publish-site')
}
```
* **问题一**：前端在设置 `publishLoading.value = true` 后，通过 Wails 发送了 `publish-site` 事件给后端，但是**后端完全没有监听这个事件**（在 `app.go` 等事件中心未注册 `EventsOn("publish-site")`），这就意味着后端不会对此进行响应并报错，也没有通知前端取消加载状态。甚至如果是通过前端直接调用如 Wails 的 Binding `DeployToGit()`，`publishLoading` 也没有在 `finally` 块里被重置为 `false`，导致按钮一直处于 loading 的转圈状态。
* **问题二**：而后端原生菜单（`boot.go`）点击“站点 -> 部署 / site.deploy”时，会通过 `emitEvent("publish-site")` 派发事件给前端，但**前端的 `MainLayout.vue` 本身也没有监听这个事件**（缺少形如 `EventsOn('publish-site', () => publish())` 的监听及处理逻辑）。这就导致了前后端的独立孤岛。

### 1.2 后端逻辑处于 Mock 阶段
通过追踪后端的 `DeployFacade` 和 `DeployService` 代码可以发现：
在 `backend/internal/service/deploy_service.go` 的 `DeployToGit` 函数中，目前的实现仅仅打印了日志：

```go
// Mock deployment logic
// In real implementation:
// 1. Build Static Site
// 2. Commit & Push to Remote Repo
s.log(ctx, "Executing git commands (Simulation)...")
s.log(ctx, "Deployment successful!")
```
所以，即便前端正确修改了调用，通过 `DeployFacade.DeployToGit()` 交互，实质上也不会出现任何网络同步、生成或 Git 推送。

---

## 2. 深入分析各平台同步策略的架构与设计

为了使同步功能能够支撑未来对于各个托管平台的支持需要，在了解原有 Gridea 和常见的静态构建项目部署模式后，未来的同步策略思路应该涵盖：生成静态资源树（Build） + 上传至远程服务器（Push）。根据平台不同，上传的手段也会有主要区分：

### 2.1 基于 Git 的同步策略 (默认核心：如 GitHub Pages / Gitee Pages)
这是 Gridea 乃至各类静态博客中最经典的免费部署方式。它不要求用户提供服务器，仅需将文件作为普通源码 Push 到 Repo。
* **实现策略**：
  1. 调用主题渲染引擎，将当前的帖子、页面与配置输出为静态的 HTML、CSS、JS 文件，均落盘至本地一个特定的 `output` 或 `.deploy_git` 缓存工作目录中。
  2. 调用 `go-git` 库（如果希望纯净 Go 实现免去命令行依赖）或通过 `exec.Command` 直接调用系统的 `git`。
  3. 执行 `git init`（若未初始化）；通过用户的 Personal Access Token 提供免密和 HTTPS 的身份验证，并将其设置为 remote。
  4. 使用当前时间或指定的信息提交更改：`git commit -m "Site updated: YYYY-MM-DD HH:MM:SS"`。
  5. 执行推送到目标分支（常见为 `gh-pages`，用户也可自定义为主分支 `main`/`master`）。需要使用 Force Push `-f`（如果只维护生成后的静态文件分支且不关心远端历史）或者 Pull + Commit + Push 规避记录断裂的问题。

### 2.2 基于 SSH/SFTP 的同步策略 (自有服务器)
很多极客或者希望控制自己数据访问速度的用户，更倾向于部署到自己的 VPS（虚拟专用服务器）。
* **实现策略**：
  1. 同样进行静态资源生成到本地。
  2. 对于 SFTP 同步，可以通过 Golang 的 `golang.org/x/crypto/ssh` 包构建连接并读写服务器目录。
  3. **传输优化逻辑**：为了避免全量传输造成的等待，可在此策略中引入对本地 `output` 文件夹内各个文件和远程服务器相同目录内文件的“大小/修改时间”对比，或通过 md5 哈希等来决定是否跳过，仅增量上传修改过的、并删除远端多余的文件。

### 2.3 基于托管平台自有构建逻辑的策略 (如 Vercel, Netlify, Cloudflare Pages)
由于前端全栈静态化与边缘计算的普及，支持 Vercel / Netlify 部署能够带来极速访问体验和 CDN 的加持。
* **策略 1 (依托 Git 间接触发构建)**：
  在客户端其实并不需要做任何事情，只需要支持 2.1 中的 Git 推送。由用户在 GitHub Repository 中授权给 Vercel，并在仓库发生 Push 时自动触发构建和发布。这是目前使用成本最低的做法，策略也只需要复用 `DeployToGit` 就可以了。
* **策略 2 (使用官方 API/CLI 直接上传 Deploy)**：
  不通过 GitHub 这个“中间商”。通过调用类似 `Vercel CLI` 或者直接在后端中封装他们的 `Deploy API`。将静态文件夹直接打包为 zip 或者按照它 API 要求的逐个文件流发送并触发即刻上线。这种方式更为极客且解耦。

### 2.4 本地文件夹同步 / 网盘同步策略方案 (Local)
* **实现策略**：
  这是一种类似导出（Export）的概念，当用户习惯将博客代码存放在 Dropbox、iCloud 或微盘中，并在其他拥有云虚拟主机的场景下实现间接同步时非常有用。
  此时“同步”实际上是一个只执行渲染、清空目标文件夹、和文件复制（Copy Dir）的纯粹的 “Build to Directory” 操作。

---

## 3. 针对“点击后一直转圈”问题的下一步修复规划

综上所属，目前的版本并没有实现这些复杂的平台逻辑支持，最迫切的是建立一套完整的、“不再一直转圈”的基础事件通信流。这也是接下来的改动预期：

1. **修正前端中 `MainLayout.vue` 的发布逻辑与错误捕捉**：
   - 移除不合理的单纯通过 `EventsEmit('publish-site')` 进行交互的方式；应寻找正确的方法直接调用后端暴露在全局作用域下如 `DeployFacade.DeployToGit()` 的 `Promise` 方法。
   - 使用异步调用（`try...catch(error)...finally`）包裹，不论结果成功还是抛错必须执行 `publishLoading.value = false`，从而终结 UI 转圈。
   - 配合使用原有的 Toast 向用户提示 Mock 逻辑中返回的结果日志或异常信息。
2. **连接原生菜单双边通信的点击事件**：
   - 因为从 Mac 原生菜单栏点击“Site -> Deploy”是由后端向前端发射的全局事件，在 `MainLayout.vue` 的 `onMounted` 钩子中增加 `EventsOn('publish-site', () => publish())` 来监听，把主动权交还前端流程，并向用户提示加载反馈。
3. **补全 DeployService 与后续架构**：
   - 在前端按钮问题解决后，将 `DeployService.DeployToGit` 里的 `Mock` 日志替换为根据配置实际触发上述各个平台的部署流程并对不同策略通过接口隔离进行解耦（类似定义一类具体的 Publisher）。
