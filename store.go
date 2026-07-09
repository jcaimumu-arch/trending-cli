package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ── 存储数据模型 ──────────────────────────────────────────────────

// StoredItem 存储的一条记录（比 Item 多 Content / FetchedAt / ContentURL）
type StoredItem struct {
	ID        string   `json:"id"`         // 去重 ID = sha1(URL)
	Source    string   `json:"source"`     // 来源
	Title     string   `json:"title"`      // 标题
	URL       string   `json:"url"`        // 链接
	Desc      string   `json:"desc"`       // 原始描述
	Heat      string   `json:"heat"`      // 热度
	Extra     string   `json:"extra"`     // 额外标签
	Content   string   `json:"content"`   // 抓取到的正文摘要
	FetchedAt string   `json:"fetched_at"`// 本次抓取时间（RFC3339）
	SavedAt   string   `json:"saved_at"`   // 首次存入时间（RFC3339）
	Tags      []string `json:"tags"`      // 标签（预留）
}

// 存储目录路径（懒初始化）
var storeDir = ""

// getStoreDir 返回存储目录路径：~/.trending-cli/data
func getStoreDir() (string, error) {
	if storeDir != "" {
		return storeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".trending-cli", "data")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	storeDir = dir
	return dir, nil
}

// hashID 用 URL 生成去重 ID
func hashID(url string) string {
	return fmt.Sprintf("%x", simpleSHA1(url))
}

// simpleSHA1 简单哈希（非加密用途，仅做去重 key）
func simpleSHA1(s string) []byte {
	// 使用 FNV-1a 替代 SHA1，避免额外依赖
	const (
		offset64 uint64 = 1469598103934665603
		prime64  uint64 = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(h >> (i * 8))
	}
	return b
}

// ── 去重索引 ──────────────────────────────────────────────────────

// loadExistingIDs 加载当前数据文件中已存在的 URL ID 集合
func loadExistingIDs(dateFile string) map[string]bool {
	ids := map[string]bool{}
	data, err := os.ReadFile(dateFile)
	if err != nil {
		return ids
	}
	var items []StoredItem
	if err := json.Unmarshal(data, &items); err != nil {
		return ids
	}
	for _, it := range items {
		ids[it.ID] = true
	}
	return ids
}

// appendStoredItems 将新条目追加到当日数据文件（JSON 数组）
func appendStoredItems(dateFile string, newItems []StoredItem) error {
	// 读取已有
	var existing []StoredItem
	if data, err := os.ReadFile(dateFile); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	// 合并
	existing = append(existing, newItems...)
	// 写回（美化格式，方便人工查看）
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dateFile, data, 0644)
}

// ── 内容抓取 ──────────────────────────────────────────────────────

// fetchContent 抓取单个 URL 的正文摘要。
// 为不同源做轻度处理：HTML 页面提取 <p> 文本，JSON/纯文本截断。
// 失败返回空字符串，不影响存储流程。
func fetchContent(client *http.Client, itemURL string) string {
	if itemURL == "" {
		return ""
	}
	resp, body, err := doGet(client, itemURL, map[string]string{"User-Agent": ua}, nil)
	if err != nil || resp.StatusCode >= 400 {
		return ""
	}
	// 尝试用 goquery 解析 HTML，提取有意义文本
	content := extractMainText(body, 500)
	if content != "" {
		return content
	}
	// 非 HTML 或提取失败，截断原始文本
	plain := strings.TrimSpace(string(body))
	if len(plain) > 500 {
		plain = plain[:500] + "..."
	}
	return plain
}

// extractMainText 用 goquery 从 HTML 中提取正文摘要
// 优先级: meta description / og:description > <p> 标签拼接 > article 文本 > 纯文本
func extractMainText(body []byte, maxLen int) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}

	// 1. 优先取 meta description / og:description
	for _, selector := range []string{
		`meta[name="description"]`,
		`meta[property="og:description"]`,
		`meta[name="twitter:description"]`,
	} {
		if content, ok := doc.Find(selector).Attr("content"); ok {
			content = strings.TrimSpace(content)
			if len(content) > 20 {
				return truncateText(content, maxLen)
			}
		}
	}

	// 2. 取 <article> 内的 <p> 段落
	var parts []string
	doc.Find("article p, main p, .markdown-body p, #readme p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len(t) > 10 {
			parts = append(parts, t)
		}
		if len(strings.Join(parts, " ")) >= maxLen {
			return
		}
	})
	if len(parts) > 0 {
		return truncateText(strings.Join(parts, " "), maxLen)
	}

	// 3. 退而求其次：所有 <p> 标签
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len(t) > 10 {
			parts = append(parts, t)
		}
		if len(strings.Join(parts, " ")) >= maxLen*2 {
			return
		}
	})
	if len(parts) > 0 {
		return truncateText(strings.Join(parts, " "), maxLen)
	}

	return ""
}

// truncateText 清理空白并截断到指定长度
func truncateText(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

// isNoiseContent 判断抓取到的 content 是否为噪音（HTML 模板 / JS 壳子等非正文）
func isNoiseContent(s string) bool {
	// 以 HTML 文档声明开头，说明没取到正文而是整个模板页
	if strings.HasPrefix(s, "<!DOCTYPE") || strings.HasPrefix(s, "<html") {
		return true
	}
	// 含大量 CSS / JS 特征但正文极短
	if strings.Contains(s, "window.__") || strings.Contains(s, "require.config") {
		return true
	}
	// 过短（不到 10 个字符）
	if len(strings.TrimSpace(s)) < 10 {
		return true
	}
	return false
}

// ── 并发抓取内容 + 存储 ───────────────────────────────────────────

// saveItems 将 fetchAll 返回的 6 组 items 存储到本地。
// 1) 按 URL 去重（当日文件 + 全局 index）
// 2) 并发抓取每个 item 的正文摘要
// 3) 追加写入 ~/.trending-cli/data/YYYY-MM-DD.json
func saveItems(github, reddit, zhihu, hackernews, v2ex, weibo []Item) (int, error) {
	dir, err := getStoreDir()
	if err != nil {
		return 0, err
	}

	// 当日数据文件
	now := time.Now()
	dateFile := filepath.Join(dir, now.Format("2006-01-02")+".json")

	// 加载当日已有 ID 做去重
	existing := loadExistingIDs(dateFile)

	// 合并所有 items
	all := make([]Item, 0, len(github)+len(reddit)+len(zhihu)+len(hackernews)+len(v2ex)+len(weibo))
	all = append(all, github...)
	all = append(all, reddit...)
	all = append(all, zhihu...)
	all = append(all, hackernews...)
	all = append(all, v2ex...)
	all = append(all, weibo...)

	// 过滤掉当日已存在的（同一天多次运行不重复）
	var toSave []StoredItem
	for _, it := range all {
		id := hashID(it.URL)
		if existing[id] {
			continue // 当日已存在
		}
		existing[id] = true // 防止同批次内重复
		toSave = append(toSave, StoredItem{
			ID:        id,
			Source:    it.Source,
			Title:     it.Title,
			URL:       it.URL,
			Desc:      it.Desc,
			Heat:      it.Heat,
			Extra:     it.Extra,
			FetchedAt: now.Format(time.RFC3339),
			SavedAt:   now.Format(time.RFC3339),
		})
	}

	if len(toSave) == 0 {
		return 0, nil
	}

	// 并发抓取正文摘要（限制并发数避免被限流）
	saveWithContent(newClient(nil), toSave)

	// 兜底：content 为空或疑似噪音时用 Desc 填充
	for i := range toSave {
		c := toSave[i].Content
		if (c == "" || isNoiseContent(c)) && toSave[i].Desc != "" {
			toSave[i].Content = toSave[i].Desc
		}
	}

	// 追加写入当日文件
	if err := appendStoredItems(dateFile, toSave); err != nil {
		return 0, err
	}

	return len(toSave), nil
}

// saveWithContent 并发抓取正文并填充到 toSave 中
func saveWithContent(client *http.Client, items []StoredItem) {
	// 限制最大并发数为 10
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items[idx].Content = fetchContent(client, items[idx].URL)
		}(i)
	}
	wg.Wait()
}

// ── 查询历史 ──────────────────────────────────────────────────────

// listStoredDates 列出所有已存储的日期
func listStoredDates() ([]string, error) {
	dir, err := getStoreDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dates []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") && len(name) == 15 { // YYYY-MM-DD.json
			dates = append(dates, name[:10])
		}
	}
	return dates, nil
}

// loadStoredByDate 加载某日的所有存储条目
func loadStoredByDate(date string) ([]StoredItem, error) {
	dir, err := getStoreDir()
	if err != nil {
		return nil, err
	}
	f := filepath.Join(dir, date+".json")
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	var items []StoredItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}
