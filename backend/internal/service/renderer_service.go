package service

import (
	"context"
	"errors"
	"fmt"
	"gridea-pro/backend/internal/domain"
	"gridea-pro/backend/internal/render"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type RendererService struct {
	postRepo     domain.PostRepository
	themeRepo    domain.ThemeRepository
	settingRepo  domain.SettingRepository
	menuRepo     domain.MenuRepository
	commentRepo  domain.CommentRepository
	linkRepo     domain.LinkRepository
	tagRepo      domain.TagRepository
	memoRepo     domain.MemoRepository
	categoryRepo domain.CategoryRepository // 用于渲染时查询分类信息
	appDir       string

	// 主题配置服务
	themeConfigService *ThemeConfigService

	// 资源管理器
	assetManager *AssetManager

	// 主题渲染器(新架构)
	renderer     render.ThemeRenderer
	currentTheme string
	logger       *slog.Logger
}

func NewRendererService(
	appDir string,
	postRepo domain.PostRepository,
	themeRepo domain.ThemeRepository,
	settingRepo domain.SettingRepository,
) *RendererService {
	themeConfigService := NewThemeConfigService(appDir)
	return &RendererService{
		postRepo:           postRepo,
		themeRepo:          themeRepo,
		settingRepo:        settingRepo,
		appDir:             appDir,
		themeConfigService: themeConfigService,
		assetManager:       NewAssetManager(appDir, themeConfigService),
		logger:             slog.Default(),
	}
}

// SetMenuRepo 设置菜单仓库（用于获取菜单数据）
func (s *RendererService) SetMenuRepo(menuRepo domain.MenuRepository) {
	s.menuRepo = menuRepo
}

// SetCommentRepo 设置评论仓库（用于获取评论设置）
func (s *RendererService) SetCommentRepo(commentRepo domain.CommentRepository) {
	s.commentRepo = commentRepo
}

// SetLinkRepo 设置友链仓库（用于渲染友链页）
func (s *RendererService) SetLinkRepo(linkRepo domain.LinkRepository) {
	s.linkRepo = linkRepo
}

// SetTagRepo 设置标签仓库
func (s *RendererService) SetTagRepo(tagRepo domain.TagRepository) {
	s.tagRepo = tagRepo
}

// SetMemoRepo 设置闪念仓库（用于渲染闪念页）
func (s *RendererService) SetMemoRepo(memoRepo domain.MemoRepository) {
	s.memoRepo = memoRepo
}

// SetCategoryRepo 设置分类仓库（用于渲染时解析分类信息）
func (s *RendererService) SetCategoryRepo(categoryRepo domain.CategoryRepository) {
	s.categoryRepo = categoryRepo
}

// SetTheme 设置主题并初始化渲染器
func (s *RendererService) SetTheme(themeName string) error {
	// 缓存检查：如果渲染器已初始化且主题未变更，直接返回
	if s.renderer != nil && s.currentTheme == themeName {
		return nil
	}

	factory := render.NewRendererFactory(s.appDir, themeName)
	renderer, err := factory.CreateRenderer()
	if err != nil {
		return fmt.Errorf("创建渲染器失败: %w", err)
	}
	s.renderer = renderer
	s.currentTheme = themeName // 更新当前主题
	s.logger.Info(fmt.Sprintf("✅ 使用 %s 引擎渲染主题: %s", renderer.GetEngineType(), themeName))
	return nil
}

func (s *RendererService) RenderAll(ctx context.Context) error {
	startTime := time.Now()
	// 获取数据
	posts, _, err := s.postRepo.List(ctx, 1, 10000) // Use List with large page size
	if err != nil {
		return fmt.Errorf("获取文章失败: %w", err)
	}

	themeConfig, err := s.themeRepo.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("获取主题配置失败: %w", err)
	}

	// 初始化渲染器
	if err := s.SetTheme(themeConfig.ThemeName); err != nil {
		return fmt.Errorf("初始化渲染器失败: %w", err)
	}

	buildDir := filepath.Join(s.appDir, DirOutput)
	// Optimization: Do NOT remove the entire directory.
	// This causes significant performance issues (3s+ delay) on every preview.
	// Overwriting files is sufficient for preview purposes.
	_ = os.MkdirAll(buildDir, 0755)

	var errs error

	// 1. 复制主题资源
	if err := s.assetManager.CopyThemeAssets(buildDir, themeConfig.ThemeName); err != nil {
		errs = errors.Join(errs, fmt.Errorf("复制主题资源失败: %w", err))
		s.logger.Error(fmt.Sprintf("警告：复制主题资源失败: %v", err))
	}

	// 2. 复制站点静态资源（images、media 等）
	if err := s.assetManager.CopySiteAssets(buildDir); err != nil {
		errs = errors.Join(errs, fmt.Errorf("复制站点资源失败: %w", err))
		s.logger.Error(fmt.Sprintf("警告：复制站点资源失败: %v", err))
	}

	// 3. 构建模板数据
	templateData, err := s.buildTemplateData(ctx, posts, themeConfig)
	if err != nil {
		return fmt.Errorf("构建模板数据失败: %w", err)
	}

	type renderTask struct {
		name string
		fn   func() error
	}

	// 核心业务：列表类页面渲染（有顺序或依赖关系的情况，或保持简单串行）
	tasks := []renderTask{
		{"首页", func() error { return s.renderIndex(ctx, buildDir, templateData) }},
		{"博客列表页", func() error { return s.renderBlog(ctx, buildDir, templateData) }},
		{"标签页", func() error { return s.renderTags(ctx, buildDir, templateData, themeConfig) }},
		{"归档页", func() error { return s.renderArchives(ctx, buildDir, templateData) }},
		{"标签文章页", func() error { return s.renderTagPages(ctx, buildDir, templateData, themeConfig) }},
	}

	for _, task := range tasks {
		if err := task.fn(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s失败: %w", task.name, err))
			s.logger.Error(fmt.Sprintf("警告：%s失败: %v", task.name, err))
		}
	}

	// 渲染文章详情页 (并发)
	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU()) // 限制并发数

	for _, post := range posts {
		if !post.Published {
			continue
		}
		p := post
		g.Go(func() error {
			if err := s.renderPost(buildDir, p, templateData); err != nil {
				s.logger.Error(fmt.Sprintf("rendering post %s: %v", p.Title, err))
				return fmt.Errorf("rendering post %s: %w", p.Title, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		errs = errors.Join(errs, err)
	}

	// 完全独立、无依赖的页面与元数据生成，运用 errgroup 并发执行
	asyncTasks := []renderTask{
		{"友链页", func() error { return s.renderFriends(ctx, buildDir, templateData) }},
		{"闪念页", func() error { return s.renderMemos(ctx, buildDir, templateData) }},
		{"404页面", func() error { return s.render404(buildDir, templateData) }},
		{"搜索数据(search.json)", func() error { return s.renderSearchJSON(buildDir, templateData) }},
		{"RSS订阅(feed.xml)", func() error { return s.renderRSS(buildDir, templateData) }},
		{"站点地图(sitemap.xml)", func() error { return s.renderSitemap(buildDir, templateData) }},
		{"Robots(robots.txt)", func() error { return s.renderRobotsTxt(buildDir, templateData) }},
	}

	asyncGroup, asyncCtx := errgroup.WithContext(ctx)
	// 可以设置合理的并发数，或者不设置
	asyncGroup.SetLimit(10)

	var asyncErrs error
	var errsMu sync.Mutex

	for _, task := range asyncTasks {
		t := task
		asyncGroup.Go(func() error {
			select {
			case <-asyncCtx.Done():
				return asyncCtx.Err()
			default:
			}
			if err := t.fn(); err != nil {
				s.logger.Error(fmt.Sprintf("警告：%s并发生成失败: %v", t.name, err))
				errsMu.Lock()
				asyncErrs = errors.Join(asyncErrs, fmt.Errorf("%s失败: %w", t.name, err))
				errsMu.Unlock()
			}
			return nil
		})
	}

	if err := asyncGroup.Wait(); err != nil {
		errs = errors.Join(errs, err)
	}
	if asyncErrs != nil {
		errs = errors.Join(errs, asyncErrs)
	}

	totalDuration := time.Since(startTime)
	s.logger.Info(fmt.Sprintf("渲染完成，共 %d 篇文章，耗时: %v", len(posts), totalDuration))
	return errs
}
