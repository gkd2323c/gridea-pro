package deployer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gridea-pro/backend/internal/domain"

	"golang.org/x/sync/errgroup"
)

// VercelProvider 实现了 Vercel API 直传部署策略
type VercelProvider struct{}

func NewVercelProvider() *VercelProvider {
	return &VercelProvider{}
}

// VercelFileResult 表示用于创建部署的文件哈希映射
type VercelFileResult struct {
	File string `json:"file"`
	Sha  string `json:"sha"`
	Size int64  `json:"size"`
}

// Deploy 实现了 Provider 接口
// 包含遍历计算 Hash，多并发上传文件以及创建部署的逻辑
func (p *VercelProvider) Deploy(ctx context.Context, outputDir string, setting *domain.Setting, logger LogFunc) error {
	logger("🚀 开始准备 Vercel 部署...")

	// 优先读取 Repository 作为项目名，如空则读取 Username
	projectName := setting.Repository
	if projectName == "" {
		projectName = setting.Username
	}
	if projectName == "" {
		return fmt.Errorf("未设置项目名称，请在设置中配置仓库名(Repository)或用户名(Username)")
	}

	token := setting.Token
	if token == "" {
		return fmt.Errorf("未设置 Vercel Token")
	}

	logger(fmt.Sprintf("Vercel 项目名称: %s", projectName))

	// 1. 遍历 outputDir，计算所以文件的 SHA1 和大小
	logger("正在扫描文件并计算 SHA1 哈希值...")
	fileResults, err := p.scanAndHashFiles(outputDir)
	if err != nil {
		return fmt.Errorf("扫描文件失败: %w", err)
	}

	if len(fileResults) == 0 {
		logger("没有发现可供部署的文件。")
		return nil
	}

	logger(fmt.Sprintf("文件扫描完成！共发现 %d 个文件准备上传。", len(fileResults)))

	// 2. 并发批量上传文件
	logger("====== 开始并发上传文件到 Vercel ======")
	if err := p.uploadFiles(ctx, outputDir, fileResults, token, logger); err != nil {
		return fmt.Errorf("并发上传文件失败: %w", err)
	}
	logger("所有文件上传完成！")

	// 3. 触发部署创建
	logger("正在触发生效部署...")
	if err := p.createDeployment(ctx, projectName, fileResults, token); err != nil {
		return fmt.Errorf("创建最新部署失败: %w", err)
	}

	logger("✅ Vercel 部署成功触发生效！")
	return nil
}

// scanAndHashFiles 遍历目录，计算每个文件的 SHA1 值及文件大小
func (p *VercelProvider) scanAndHashFiles(outputDir string) ([]VercelFileResult, error) {
	var results []VercelFileResult

	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// 计算相对路径，API 要求正斜杠 (/)
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		// 读取文件并计算哈希
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		hash := sha1.New()
		if _, err := io.Copy(hash, file); err != nil {
			return err
		}
		shaStr := hex.EncodeToString(hash.Sum(nil))

		results = append(results, VercelFileResult{
			File: relPath,
			Sha:  shaStr,
			Size: info.Size(),
		})

		return nil
	})

	return results, err
}

// uploadFiles 并发地通过 Vercel v2 接口上传文件
func (p *VercelProvider) uploadFiles(ctx context.Context, outputDir string, files []VercelFileResult, token string, logger LogFunc) error {
	// 使用 errgroup 限制最大并发并发数为 10
	var eg errgroup.Group
	eg.SetLimit(10)

	for _, result := range files {
		// 复制循环变量以便在 goroutine 中安全使用
		res := result
		eg.Go(func() error {
			// 检查 ctx 是否已经被用户取消
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			filePath := filepath.Join(outputDir, filepath.FromSlash(res.File))
			if err := p.uploadSingleFile(ctx, filePath, res.Sha, res.Size, token); err != nil {
				return fmt.Errorf("文件 %s 上传失败: %w", res.File, err)
			}

			logger(fmt.Sprintf("文件就绪: %s", res.File))
			return nil
		})
	}

	// 阻塞等待直到所有协程执行完成
	return eg.Wait()
}

// uploadSingleFile 向 API 接口上传单一文件，并在头部传递特征
func (p *VercelProvider) uploadSingleFile(ctx context.Context, filePath, sha string, size int64, token string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Vercel v2 file upload endpoints
	// POST https://api.vercel.com/v2/files
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.vercel.com/v2/files", file)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-vercel-digest", sha)
	req.ContentLength = size

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 解析出非成功状态 (Vercel 会对有效但不变更的文件返回 2xx 或直接不修改，取决于具体响应)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d, 响应报文: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// createDeployment 请求 Vercel v13 的部署接口以提交刚才记录的整个依赖信息和项目结构，并建立部署进程
func (p *VercelProvider) createDeployment(ctx context.Context, projectName string, files []VercelFileResult, token string) error {
	// 构建 Payload
	payload := map[string]interface{}{
		"name":  projectName,
		"files": files,
		"projectSettings": map[string]interface{}{
			"framework": nil, // 使用静态框架托管
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// POST https://api.vercel.com/v13/deployments
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.vercel.com/v13/deployments", bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d, 创建部署响应: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
