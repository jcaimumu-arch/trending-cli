package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ── Web Server ───────────────────────────────────────────────────

// webItem 是 web 展示用的去重后条目
type webItem struct {
	StoredItem
	Dates   []string `json:"dates"`   // 出现在哪些日期
	Count   int      `json:"count"`   // 出现次数
	LastSee string   `json:"lastSee"` // 最后一次出现日期
}

// startWebServer 启动 HTTP 服务器展示历史数据
func startWebServer(addr string) error {
	// 确保存储目录存在
	dir, err := getStoreDir()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	// API: AI 分析
	mux.HandleFunc("/api/analyze", func(w http.ResponseWriter, r *http.Request) {
		days := 7
		if d := r.URL.Query().Get("days"); d != "" {
			if n, err := parseInt(d); err == nil && n > 0 {
				days = n
			}
		}
		result, err := analyzeTrends(days)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// 分析页面
	mux.HandleFunc("/analysis", func(w http.ResponseWriter, r *http.Request) {
		renderAnalysisPage(w)
	})

	// 首页：汇总去重后的所有条目
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		items, dates, err := loadAllItems(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// 查询参数过滤
		source := r.URL.Query().Get("source")
		q := r.URL.Query().Get("q")
		dateFilter := r.URL.Query().Get("date")

		var filtered []webItem
		for _, it := range items {
			if source != "" && it.Source != source {
				continue
			}
			if q != "" {
				titleLower := strings.ToLower(it.Title)
				if !strings.Contains(titleLower, strings.ToLower(q)) {
					continue
				}
			}
			if dateFilter != "" {
				found := false
				for _, d := range it.Dates {
					if d == dateFilter {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			filtered = append(filtered, it)
		}

		renderHTML(w, filtered, dates)
	})

	fmt.Printf("Web 服务启动: http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// loadAllItems 加载所有日期文件，按 URL 去重合并
func loadAllItems(dir string) ([]webItem, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	// 收集所有日期
	var dateFiles []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") && len(name) == 15 {
			dateFiles = append(dateFiles, name[:10])
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dateFiles)))

	// URL -> webItem 合并
	itemMap := map[string]*webItem{}

	for _, date := range dateFiles {
		f := filepath.Join(dir, date+".json")
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var items []StoredItem
		if err := json.Unmarshal(data, &items); err != nil {
			continue
		}
		for _, it := range items {
			key := it.URL
			if existing, ok := itemMap[key]; ok {
				existing.Dates = append(existing.Dates, date)
				existing.Count++
				if date > existing.LastSee {
					existing.LastSee = date
				}
			} else {
				wi := webItem{
					StoredItem: it,
					Dates:      []string{date},
					Count:      1,
					LastSee:    date,
				}
				itemMap[key] = &wi
			}
		}
	}

	// 转为 slice 并按出现次数排序
	var result []webItem
	for _, wi := range itemMap {
		result = append(result, *wi)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].LastSee > result[j].LastSee
	})

	return result, dateFiles, nil
}

// renderHTML 渲染 HTML 页面
func renderHTML(w http.ResponseWriter, items []webItem, dates []string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>热榜汇总</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; color: #333; }
  .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 24px 0; text-align: center; }
  .header h1 { font-size: 1.8em; margin-bottom: 4px; }
  .header h1 a { color: white; text-decoration: none; }
  .header .sub { font-size: 0.9em; opacity: 0.85; }
  .nav { text-align: center; margin-top: 8px; }
  .nav a { color: rgba(255,255,255,0.85); text-decoration: none; margin: 0 12px; font-size: 0.85em; }
  .nav a:hover { color: white; }
  .container { max-width: 1000px; margin: 0 auto; padding: 20px; }
  .toolbar { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; align-items: center; }
  .toolbar select, .toolbar input { padding: 8px 12px; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; }
  .toolbar input[type="text"] { flex: 1; min-width: 200px; }
  .toolbar button { padding: 8px 16px; background: #667eea; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; }
  .toolbar button:hover { background: #5568d3; }
  .stats { color: #888; font-size: 0.85em; margin-bottom: 16px; }
  .item { background: white; border-radius: 8px; padding: 16px 20px; margin-bottom: 10px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); transition: box-shadow 0.2s; }
  .item:hover { box-shadow: 0 2px 8px rgba(0,0,0,0.15); }
  .item-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; flex-wrap: wrap; }
  .source-tag { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.75em; font-weight: 600; color: white; }
  .src-GitHub { background: #24292e; }
  .src-Reddit { background: #ff4500; }
  .src-知乎 { background: #0084ff; }
  .src-HackerNews { background: #ff6600; }
  .src-V2EX { background: #333; }
  .src-微博 { background: #e6162d; }
  .count-badge { background: #667eea; color: white; padding: 2px 8px; border-radius: 10px; font-size: 0.75em; }
  .item-title { font-size: 1.05em; font-weight: 500; }
  .item-title a { color: #1a1a1a; text-decoration: none; }
  .item-title a:hover { color: #667eea; }
  .item-meta { font-size: 0.8em; color: #999; margin-top: 4px; }
  .item-content { font-size: 0.85em; color: #666; margin-top: 6px; line-height: 1.5; }
  .heat { color: #e74c3c; font-weight: 600; font-size: 0.8em; }
  .dates { font-size: 0.75em; color: #aaa; margin-top: 4px; }
  .empty { text-align: center; padding: 40px; color: #999; }
</style>
</head>
<body>
<div class="header">
  <h1><a href="/">热榜汇总</a></h1>
  <div class="sub">历史数据去重聚合 · 共 `)
	b.WriteString(fmt.Sprintf("%d", len(items)))
	b.WriteString(` 条</div>
  <div class="nav">
    <a href="/">首页</a>
    <a href="/analysis">趋势分析</a>
  </div>
</div>
<div class="container">
  <form class="toolbar" method="GET" action="/">
    <select name="source">
      <option value="">全部来源</option>
      <option value="GitHub">GitHub</option>
      <option value="Reddit">Reddit</option>
      <option value="知乎">知乎</option>
      <option value="HackerNews">HackerNews</option>
      <option value="V2EX">V2EX</option>
      <option value="微博">微博</option>
    </select>
    <select name="date">
      <option value="">全部日期</option>`)
	for _, d := range dates {
		b.WriteString(fmt.Sprintf(`<option value="%s">%s</option>`, d, d))
	}
	b.WriteString(`</select>
    <input type="text" name="q" placeholder="搜索标题..." />
    <button type="submit">筛选</button>
  </form>
  <div class="stats">共 `)
	b.WriteString(fmt.Sprintf("%d", len(items)))
	b.WriteString(` 条 · 数据来源 ~/.trending-cli/data/</div>
`)

	if len(items) == 0 {
		b.WriteString(`<div class="empty">暂无数据，运行 <code>trending --save</code> 开始采集</div>`)
	} else {
		for _, it := range items {
			srcClass := "src-" + it.Source
			b.WriteString(fmt.Sprintf(`<div class="item">
  <div class="item-header">
    <span class="source-tag %s">%s</span>
    <span class="count-badge" title="出现次数">%d 次</span>
    %s
  </div>
  <div class="item-title"><a href="%s" target="_blank">%s</a></div>
  <div class="item-meta">%s <span class="heat">%s</span></div>
  <div class="item-content">%s</div>
  <div class="dates">出现于: %s · 最后出现: %s</div>
</div>`,
				srcClass,
				it.Source,
				it.Count,
				func() string {
					if it.Heat != "" {
						return `<span class="heat">` + it.Heat + `</span>`
					}
					return ""
				}(),
				it.URL,
				escapeHTML(it.Title),
				it.Extra,
				it.Heat,
				escapeHTML(truncateStr(it.Content, 200)),
				strings.Join(it.Dates, ", "),
				it.LastSee,
			))
		}
	}

	b.WriteString(`
</div>
</body>
</html>`)
	fmt.Fprint(w, b.String())
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// renderAnalysisPage 渲染趋势分析页面（AI 分析 + 图表）
func renderAnalysisPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>趋势分析 - 热榜汇总</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.1/dist/chart.umd.min.js"></script>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; color: #333; }
  .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 24px 0; text-align: center; }
  .header h1 { font-size: 1.8em; margin-bottom: 4px; }
  .header h1 a { color: white; text-decoration: none; }
  .header .sub { font-size: 0.9em; opacity: 0.85; }
  .nav { text-align: center; margin-top: 8px; }
  .nav a { color: rgba(255,255,255,0.85); text-decoration: none; margin: 0 12px; font-size: 0.85em; }
  .nav a:hover { color: white; }
  .container { max-width: 1100px; margin: 0 auto; padding: 20px; }
  .card { background: white; border-radius: 8px; padding: 20px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }
  .card h2 { font-size: 1.1em; margin-bottom: 12px; color: #555; }
  .chart-row { display: flex; gap: 20px; flex-wrap: wrap; }
  .chart-box { flex: 1; min-width: 300px; }
  .controls { display: flex; gap: 12px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
  .controls select { padding: 6px 12px; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; }
  .btn { padding: 8px 20px; background: #667eea; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; }
  .btn:hover { background: #5568d3; }
  .btn:disabled { background: #ccc; cursor: not-allowed; }
  .analysis-card { background: white; border-radius: 8px; padding: 20px; margin-bottom: 12px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }
  .analysis-card h3 { font-size: 1em; color: #667eea; margin-bottom: 8px; }
  .analysis-card p { line-height: 1.7; color: #555; }
  .loading { text-align: center; padding: 40px; color: #999; }
  .error-msg { color: #e74c3c; text-align: center; padding: 20px; }
  .placeholder { text-align: center; padding: 60px 20px; color: #aaa; }
  .placeholder p { margin-top: 8px; font-size: 0.9em; }
  .topic-table { width: 100%; border-collapse: collapse; font-size: 0.85em; }
  .topic-table th { text-align: left; padding: 8px 12px; border-bottom: 2px solid #eee; color: #888; }
  .topic-table td { padding: 8px 12px; border-bottom: 1px solid #f0f0f0; }
  .heat-bar { display: inline-block; width: 60px; height: 8px; background: #eee; border-radius: 4px; overflow: hidden; }
  .heat-fill { height: 100%; border-radius: 4px; }
  .sentiment-pos { color: #27ae60; font-weight: 600; }
  .sentiment-neg { color: #e74c3c; font-weight: 600; }
  .sentiment-neu { color: #999; font-weight: 600; }
  .category-tag { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 0.8em; background: #f0f0f5; color: #666; }
  .result-section { display: none; }
  .result-section.visible { display: block; }
</style>
</head>
<body>
<div class="header">
  <h1><a href="/">趋势分析</a></h1>
  <div class="sub">AI 驱动的热点趋势分析与数据可视化</div>
  <div class="nav">
    <a href="/">首页</a>
    <a href="/analysis">趋势分析</a>
  </div>
</div>
<div class="container">
  <div class="controls">
    <label>分析范围：</label>
    <select id="days">
      <option value="3">最近 3 天</option>
      <option value="7" selected>最近 7 天</option>
      <option value="14">最近 14 天</option>
      <option value="30">最近 30 天</option>
    </select>
    <button class="btn" id="analyzeBtn">开始 AI 分析</button>
  </div>

  <div id="placeholder" class="placeholder">
    <div style="font-size: 3em;">📊</div>
    <p>点击「开始 AI 分析」，AI 将分析近期热榜数据并生成趋势图表</p>
  </div>

  <div id="resultSection" class="result-section">
    <!-- 总体摘要 -->
    <div class="analysis-card">
      <h3>总体趋势摘要</h3>
      <p id="summaryText"></p>
    </div>

    <!-- 图表区域 -->
    <div class="chart-row">
      <div class="card chart-box">
        <h2>话题热度排行</h2>
        <canvas id="heatChart"></canvas>
      </div>
      <div class="card chart-box">
        <h2>分类分布</h2>
        <canvas id="categoryChart"></canvas>
      </div>
    </div>

    <div class="chart-row">
      <div class="card chart-box">
        <h2>舆论情绪分布</h2>
        <canvas id="sentimentChart"></canvas>
      </div>
      <div class="card chart-box">
        <h2>分类热度对比</h2>
        <canvas id="categoryHeatChart"></canvas>
      </div>
    </div>

    <!-- 话题详情表 -->
    <div class="card">
      <h2>热点话题详情</h2>
      <table class="topic-table">
        <thead><tr><th>#</th><th>话题</th><th>分类</th><th>热度</th><th>情绪</th><th>描述</th></tr></thead>
        <tbody id="topicTableBody"></tbody>
      </table>
    </div>

    <!-- 文字分析 -->
    <div class="analysis-card">
      <h3>技术趋势</h3>
      <p id="techTrendText"></p>
    </div>
    <div class="analysis-card">
      <h3>舆论情绪分析</h3>
      <p id="sentimentText"></p>
    </div>
    <div class="analysis-card">
      <h3>建议</h3>
      <p id="suggestionText"></p>
    </div>
    <div id="generatedAtInfo" style="text-align:right;color:#aaa;font-size:0.75em;margin-top:4px;"></div>
  </div>
</div>

<script>
let heatChart = null, categoryChart = null, sentimentChart = null, categoryHeatChart = null;

function escapeHtml(s) {
  if (typeof s !== 'string') return '';
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

const categoryColors = {
  '技术': '#667eea', '社会': '#e74c3c', '娱乐': '#f39c12',
  '经济': '#27ae60', '体育': '#3498db', '科学': '#9b59b6',
  '其他': '#95a5a6'
};
const sentimentColors = { 'positive': '#27ae60', 'negative': '#e74c3c', 'neutral': '#95a5a6' };
const sentimentLabels = { 'positive': '正面', 'negative': '负面', 'neutral': '中性' };
const sentimentClasses = { 'positive': 'sentiment-pos', 'negative': 'sentiment-neg', 'neutral': 'sentiment-neu' };

function renderHeatChart(topics) {
  const ctx = document.getElementById('heatChart').getContext('2d');
  const sorted = [...topics].sort((a, b) => b.heatScore - a.heatScore).slice(0, 10);
  const labels = sorted.map(t => t.name.length > 12 ? t.name.slice(0, 12) + '…' : t.name);
  const data = sorted.map(t => t.heatScore);
  const colors = sorted.map(t => categoryColors[t.category] || '#999');
  if (heatChart) heatChart.destroy();
  heatChart = new Chart(ctx, {
    type: 'bar',
    data: { labels, datasets: [{ label: '热度分数', data, backgroundColor: colors, borderRadius: 4 }] },
    options: { indexAxis: 'y', responsive: true, scales: { x: { max: 100 } },
      plugins: { legend: { display: false } } }
  });
}

function renderCategoryChart(cats) {
  const ctx = document.getElementById('categoryChart').getContext('2d');
  const labels = cats.map(c => c.category);
  const data = cats.map(c => c.count);
  const colors = labels.map(l => categoryColors[l] || '#999');
  if (categoryChart) categoryChart.destroy();
  categoryChart = new Chart(ctx, {
    type: 'doughnut',
    data: { labels, datasets: [{ data, backgroundColor: colors }] },
    options: { responsive: true }
  });
}

function renderSentimentChart(dist) {
  const ctx = document.getElementById('sentimentChart').getContext('2d');
  if (sentimentChart) sentimentChart.destroy();
  sentimentChart = new Chart(ctx, {
    type: 'doughnut',
    data: { labels: ['正面', '负面', '中性'],
      datasets: [{ data: [dist.positive, dist.negative, dist.neutral],
        backgroundColor: ['#27ae60', '#e74c3c', '#95a5a6'] }] },
    options: { responsive: true }
  });
}

function renderCategoryHeatChart(topics) {
  const ctx = document.getElementById('categoryHeatChart').getContext('2d');
  const catMap = {};
  topics.forEach(t => {
    if (!catMap[t.category]) catMap[t.category] = [];
    catMap[t.category].push(t.heatScore);
  });
  const labels = Object.keys(catMap);
  const avgData = labels.map(c => {
    const scores = catMap[c];
    return Math.round(scores.reduce((a, b) => a + b, 0) / scores.length);
  });
  const colors = labels.map(l => categoryColors[l] || '#999');
  if (categoryHeatChart) categoryHeatChart.destroy();
  categoryHeatChart = new Chart(ctx, {
    type: 'bar',
    data: { labels, datasets: [{ label: '平均热度', data: avgData, backgroundColor: colors, borderRadius: 4 }] },
    options: { responsive: true, scales: { y: { max: 100 } },
      plugins: { legend: { display: false } } }
  });
}

function renderTopicTable(topics) {
  const tbody = document.getElementById('topicTableBody');
  const sorted = [...topics].sort((a, b) => b.heatScore - a.heatScore);
  tbody.innerHTML = sorted.map((t, i) => {
    const sc = sentimentColors[t.sentiment] || '#999';
    const sl = sentimentLabels[t.sentiment] || t.sentiment;
    const scls = sentimentClasses[t.sentiment] || '';
    const catColor = categoryColors[t.category] || '#999';
    return '<tr>' +
      '<td>' + (i + 1) + '</td>' +
      '<td style="font-weight:500;">' + escapeHtml(t.name) + '</td>' +
      '<td><span class="category-tag" style="background:' + catColor + '22;color:' + catColor + ';">' + escapeHtml(t.category) + '</span></td>' +
      '<td><div class="heat-bar"><div class="heat-fill" style="width:' + t.heatScore + '%;background:' + sc + ';"></div></div> ' + t.heatScore + '</td>' +
      '<td class="' + scls + '">' + sl + '</td>' +
      '<td style="color:#777;">' + escapeHtml(t.summary || '') + '</td>' +
      '</tr>';
  }).join('');
}

function renderResult(result) {
  document.getElementById('placeholder').style.display = 'none';
  document.getElementById('resultSection').classList.add('visible');

  document.getElementById('summaryText').textContent = result.summary || '';
  document.getElementById('techTrendText').textContent = result.techTrend || '';
  document.getElementById('sentimentText').textContent = result.sentiment || '';
  document.getElementById('suggestionText').textContent = result.suggestion || '';

  if (result.generatedAt) {
    const d = new Date(result.generatedAt);
    document.getElementById('generatedAtInfo').textContent = '生成时间: ' + d.toLocaleString('zh-CN');
  }

  if (result.hotTopics) renderHeatChart(result.hotTopics);
  if (result.hotTopics) renderCategoryHeatChart(result.hotTopics);
  if (result.hotTopics) renderTopicTable(result.hotTopics);
  if (result.categoryDist) renderCategoryChart(result.categoryDist);
  if (result.sentimentDist) renderSentimentChart(result.sentimentDist);
}

document.getElementById('analyzeBtn').addEventListener('click', async function() {
  const btn = this;
  const days = document.getElementById('days').value;
  btn.disabled = true;
  btn.textContent = 'AI 分析中...';
  document.getElementById('placeholder').innerHTML = '<div class="loading">正在调用 AI 分析近期趋势，请稍候（约 10-20 秒）...</div>';
  try {
    const resp = await fetch('/api/analyze?days=' + days);
    const result = await resp.json();
    if (result.error) {
      document.getElementById('placeholder').innerHTML = '<div class="error-msg">' + escapeHtml(result.error) + '</div>';
    } else {
      renderResult(result);
    }
  } catch(e) {
    document.getElementById('placeholder').innerHTML = '<div class="error-msg">请求失败: ' + escapeHtml(e.message) + '</div>';
  }
  btn.disabled = false;
  btn.textContent = '重新分析';
});
</script>
</body>
</html>`

	fmt.Fprint(w, html)
}
