package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── 配置管理 ──────────────────────────────────────────────────────

// AIConfig AI 配置
type AIConfig struct {
	Provider       string `json:"provider"` // "gemini" 或 "deepseek"
	GeminiAPIKey   string `json:"gemini_api_key"`
	DeepSeekAPIKey string `json:"deepseek_api_key"`
	Model          string `json:"model"`
}

// defaultConfig 返回默认配置
func defaultConfig() AIConfig {
	return AIConfig{
		Provider: "deepseek",
		Model:    "deepseek-v4-flash",
	}
}

// loadConfig 从 ~/.trending-cli/config.json 加载配置
func loadConfig() (AIConfig, error) {
	cfg := defaultConfig()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, err
	}
	f := filepath.Join(home, ".trending-cli", "config.json")
	data, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，尝试环境变量
			applyEnvConfig(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	_ = json.Unmarshal(data, &cfg)

	// 环境变量优先
	applyEnvConfig(&cfg)

	// 根据_provider 设置默认 model
	if cfg.Model == "" {
		switch cfg.Provider {
		case "gemini":
			cfg.Model = "gemini-2.0-flash"
		case "deepseek":
			cfg.Model = "deepseek-v4-flash"
		default:
			cfg.Provider = "deepseek"
			cfg.Model = "deepseek-v4-flash"
		}
	}
	return cfg, nil
}

// applyEnvConfig 用环境变量覆盖配置
func applyEnvConfig(cfg *AIConfig) {
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		cfg.GeminiAPIKey = key
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		cfg.DeepSeekAPIKey = key
	}
	// 如果有 DeepSeek key 但没有 Gemini key，默认用 deepseek
	if cfg.DeepSeekAPIKey != "" && cfg.GeminiAPIKey == "" {
		cfg.Provider = "deepseek"
	}
	// 如果只有 Gemini key，默认用 gemini
	if cfg.GeminiAPIKey != "" && cfg.DeepSeekAPIKey == "" && cfg.Provider == "" {
		cfg.Provider = "gemini"
	}
}

// saveConfig 保存配置到 ~/.trending-cli/config.json
func saveConfig(cfg AIConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".trending-cli")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}

// ── AI 调用统一入口 ──────────────────────────────────────────────

// callAI 调用 AI 生成内容（根据 provider 自动分发）
func callAI(cfg AIConfig, prompt string) (string, error) {
	switch cfg.Provider {
	case "gemini":
		return callGemini(cfg, prompt)
	case "deepseek":
		return callDeepSeek(cfg, prompt)
	default:
		return "", fmt.Errorf("不支持的 AI provider: %s（可选: gemini / deepseek）", cfg.Provider)
	}
}

// ── Gemini API 调用 ──────────────────────────────────────────────

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func callGemini(cfg AIConfig, prompt string) (string, error) {
	if cfg.GeminiAPIKey == "" {
		return "", fmt.Errorf("未配置 Gemini API Key，请在 ~/.trending-cli/config.json 中设置 gemini_api_key，或设置 GEMINI_API_KEY 环境变量")
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		cfg.Model, cfg.GeminiAPIKey)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: geminiGenConfig{
			Temperature:     0.7,
			MaxOutputTokens: 4096,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := newClient(nil)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		var errResp geminiResponse
		_ = json.Unmarshal(body, &errResp)
		return "", fmt.Errorf("Gemini API 错误 (HTTP %d): %s", resp.StatusCode, errResp.Error.Message)
	}

	var result geminiResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini 返回空结果")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// ── DeepSeek API 调用（OpenAI 兼容格式） ──────────────────────────

type deepseekRequest struct {
	Model       string            `json:"model"`
	Messages    []deepseekMessage `json:"messages"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	//responseFormat *deepseekRespFormat `json:"response_format,omitempty"`
}

type deepseekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepseekResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func callDeepSeek(cfg AIConfig, prompt string) (string, error) {
	if cfg.DeepSeekAPIKey == "" {
		return "", fmt.Errorf("未配置 DeepSeek API Key，请在 ~/.trending-cli/config.json 中设置 deepseek_api_key，或设置 DEEPSEEK_API_KEY 环境变量")
	}

	model := cfg.Model
	if model == "" {
		model = "deepseek-v4-flash"
	}

	reqBody := deepseekRequest{
		Model: model,
		Messages: []deepseekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := newClient(nil)
	req, err := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.DeepSeekAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		var errResp deepseekResponse
		_ = json.Unmarshal(body, &errResp)
		msg := errResp.Error.Message
		if msg == "" {
			msg = string(body)
		}
		return "", fmt.Errorf("DeepSeek API 错误 (HTTP %d): %s", resp.StatusCode, msg)
	}

	var result deepseekResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("DeepSeek 返回空结果")
	}

	return result.Choices[0].Message.Content, nil
}

// ── 热点趋势分析 ──────────────────────────────────────────────────

// TopicTrend AI 分析出的话题趋势项
type TopicTrend struct {
	Name      string `json:"name"`
	Category  string `json:"category"`  // 分类：技术/社会/娱乐/经济/体育等
	HeatScore int    `json:"heatScore"` // 热度分数 0-100
	Sentiment string `json:"sentiment"` // positive / negative / neutral
	Summary   string `json:"summary"`   // 一句话描述
}

// CategoryDist 分类分布
type CategoryDist struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// SentimentDist 情绪分布
type SentimentDist struct {
	Positive int `json:"positive"`
	Negative int `json:"negative"`
	Neutral  int `json:"neutral"`
}

// AnalysisResult AI 分析结果
type AnalysisResult struct {
	Summary       string         `json:"summary"`
	HotTopics     []TopicTrend   `json:"hotTopics"`
	CategoryDist  []CategoryDist `json:"categoryDist"`
	SentimentDist SentimentDist  `json:"sentimentDist"`
	TechTrend     string         `json:"techTrend"`
	Sentiment     string         `json:"sentiment"`
	Suggestion    string         `json:"suggestion"`
	GeneratedAt   string         `json:"generatedAt"`
}

// analyzeTrends 分析近期热点趋势
func analyzeTrends(days int) (*AnalysisResult, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	dir, err := getStoreDir()
	if err != nil {
		return nil, err
	}

	// 加载最近 N 天的数据
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type dateData struct {
		date  string
		items []StoredItem
	}
	var allData []dateData
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || len(name) != 15 {
			continue
		}
		date := name[:10]
		data, err := os.ReadFile(filepath.Join(dir, date+".json"))
		if err != nil {
			continue
		}
		var items []StoredItem
		if err := json.Unmarshal(data, &items); err != nil {
			continue
		}
		allData = append(allData, dateData{date: date, items: items})
	}

	// 按日期排序，取最近 N 天
	for i := 0; i < len(allData); i++ {
		for j := i + 1; j < len(allData); j++ {
			if allData[i].date < allData[j].date {
				allData[i], allData[j] = allData[j], allData[i]
			}
		}
	}
	if len(allData) > days {
		allData = allData[:days]
	}

	if len(allData) == 0 {
		return nil, fmt.Errorf("没有历史数据可分析，请先运行 trending --save 采集数据")
	}

	// 构造分析用的数据摘要
	var dataSummary strings.Builder
	dataSummary.WriteString("以下是近期的热榜数据（按日期分组）：\n\n")
	for _, dd := range allData {
		dataSummary.WriteString(fmt.Sprintf("## %s\n", dd.date))
		sourceMap := map[string][]StoredItem{}
		for _, it := range dd.items {
			sourceMap[it.Source] = append(sourceMap[it.Source], it)
		}
		for source, items := range sourceMap {
			dataSummary.WriteString(fmt.Sprintf("### %s (%d 条)\n", source, len(items)))
			for _, it := range items {
				line := fmt.Sprintf("- %s", it.Title)
				if it.Heat != "" {
					line += " [" + it.Heat + "]"
				}
				if it.Content != "" {
					c := it.Content
					if len(c) > 150 {
						c = c[:150] + "..."
					}
					line += " | " + c
				}
				dataSummary.WriteString(line + "\n")
			}
		}
		dataSummary.WriteString("\n")
	}

	prompt := fmt.Sprintf(`你是一个资深的数据分析师和舆论趋势观察员。请基于以下近 %d 天的热榜数据进行分析，返回 JSON 格式的结果。

%s

请分析以下维度：
1. summary: 总体趋势摘要（200 字以内），概括近期热点全貌
2. hotTopics: 热点话题列表（8-15 个），每个包含 name(话题名)、category(分类: 技术/社会/娱乐/经济/体育/科学/其他)、heatScore(热度分数 0-100)、sentiment(positive/negative/neutral)、summary(一句话描述)
3. categoryDist: 按分类统计分布，每个包含 category 和 count
4. sentimentDist: 情绪分布统计，包含 positive、negative、neutral 的数量
5. techTrend: 技术趋势分析（GitHub/HackerNews 中的技术热点，100 字以内）
6. sentiment: 舆论情绪分析（整体偏正面/负面/中性，以及主要关注方向，100 字以内）
7. suggestion: 给开发者/投资者的建议（100 字以内）

请严格返回以下 JSON 格式（不要包含 markdown 代码块标记，不要有额外文字）：
{"summary":"...","hotTopics":[{"name":"...","category":"技术","heatScore":85,"sentiment":"positive","summary":"..."}],"categoryDist":[{"category":"技术","count":5}],"sentimentDist":{"positive":3,"negative":2,"neutral":5},"techTrend":"...","sentiment":"...","suggestion":"..."}`, days, dataSummary.String())

	result, err := callAI(cfg, prompt)
	if err != nil {
		return nil, err
	}

	// 清理可能的 markdown 代码块标记
	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```json")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	var analysis AnalysisResult
	if err := json.Unmarshal([]byte(result), &analysis); err != nil {
		analysis = AnalysisResult{
			Summary:     result,
			GeneratedAt: time.Now().Format(time.RFC3339),
		}
	}
	analysis.GeneratedAt = time.Now().Format(time.RFC3339)

	return &analysis, nil
}
