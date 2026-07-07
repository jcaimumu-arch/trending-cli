package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/proxy"
	"golang.org/x/term"
)

// ── 数据模型 ──────────────────────────────────────────────────────

// Item 一条热榜条目
type Item struct {
	Source string // 来源名
	Rank   int    // 排名
	Title  string // 标题
	URL    string // 链接
	Desc   string // 描述 / 副信息
	Heat   string // 热度数值字符串
	Extra  string // 额外标签
}

const version = "1.1.0-go"

const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) " +
	"Chrome/125.0.0.0 Safari/537.36"

// 全局可配置项（由命令行参数 / 环境变量设置）
var (
	proxyAddr     string // --proxy 指定的代理地址
	httpTimeout   = 30 * time.Second
	detectedProxy string // 启动时探测到的本地 Clash 代理
)

// proxyURL 返回生效的代理地址，优先级：
//  1. --proxy 命令行参数
//  2. 环境变量 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY
//  3. 自动探测到的本地 Clash 代理
func proxyURL() string {
	if proxyAddr != "" {
		return proxyAddr
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return detectedProxy
}

// detectLocalProxy 快速探测本地 Clash 代理端口，返回可用地址。
// 探测的端口均为 Clash / Clash Verge / Mihomo 常用默认端口。
func detectLocalProxy() string {
	candidates := []string{"127.0.0.1:7897", "127.0.0.1:7890", "127.0.0.1:7891"}
	for _, addr := range candidates {
		// 用极短超时（150ms）做 TCP 连通性探测，不阻塞太久
		conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
		if err == nil {
			conn.Close()
			return "http://" + addr
		}
	}
	return ""
}

// newClient 构造带 UA / 超时 / 代理的 HTTP 客户端
func newClient(headers map[string]string) *http.Client {
	transport := &http.Transport{
		ResponseHeaderTimeout: httpTimeout,
	}
	if p := proxyURL(); p != "" {
		if u, err := url.Parse(p); err == nil {
			switch u.Scheme {
			case "socks5", "socks5h":
				// socks5 代理：使用内置 DialContext 方式
				transport.Proxy = nil
				transport.DialContext = socks5DialContext(u)
			default:
				// http / https 代理
				transport.Proxy = http.ProxyURL(u)
			}
		}
	}
	c := &http.Client{
		Timeout:   httpTimeout,
		Transport: transport,
	}
	return c
}

// doGet 发送 GET 请求并返回 body / response
func doGet(c *http.Client, rawURL string, headers map[string]string, query map[string]string) (*http.Response, []byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	if headers != nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	if _, ok := headers["User-Agent"]; !ok {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

// getJSON 便捷方法：GET 并解析 JSON
func getJSON(c *http.Client, rawURL string, headers map[string]string, query map[string]string, v interface{}) (*http.Response, error) {
	resp, body, err := doGet(c, rawURL, headers, query)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode >= 400 {
		return resp, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return resp, err
	}
	return resp, nil
}

// socks5DialContext 返回一个通过 socks5 代理建立的 DialContext 函数。
// 支持 socks5://[user:pass@]host:port
func socks5DialContext(u *url.URL) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return d.Dial(network, addr)
	}
}

// ── 抓取函数 ─────────────────────────────────────────────────────
// 每个源一个函数，失败返回空切片并打印错误到 stderr

// fetchGitHub GitHub Trending — HTML 解析
func fetchGitHub(n int) []Item {
	c := newClient(nil)
	resp, body, err := doGet(c, "https://github.com/trending", nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[GitHub] 错误: %v\n", err)
		return nil
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "[GitHub] 错误: HTTP %d\n", resp.StatusCode)
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[GitHub] 错误: %v\n", err)
		return nil
	}
	var items []Item
	spaceRe := regexp.MustCompile(`\s+`)
	doc.Find("article.Box-row").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if i >= n {
			return false
		}
		h2 := s.Find("h2 a").First()
		if h2.Length() == 0 {
			return true
		}
		repo := spaceRe.ReplaceAllString(strings.TrimSpace(h2.Text()), "")
		u := "https://github.com/" + repo
		desc := ""
		if p := s.Find("p").First(); p.Length() > 0 {
			desc = strings.TrimSpace(p.Text())
		}
		stars := ""
		if a := s.Find(`a[href$="/stargazers"]`).First(); a.Length() > 0 {
			stars = strings.TrimSpace(a.Text())
		}
		lang := ""
		if l := s.Find("[itemprop='programmingLanguage']").First(); l.Length() > 0 {
			lang = strings.TrimSpace(l.Text())
		}
		extra := ""
		if lang != "" {
			extra = lang
		}
		items = append(items, Item{
			Source: "GitHub", Rank: i + 1, Title: repo, URL: u,
			Desc: desc, Heat: stars, Extra: extra,
		})
		return true
	})
	return items
}

// fetchReddit Reddit r/popular — JSON API
func fetchReddit(n int) []Item {
	headers := map[string]string{
		"User-Agent": ua,
		"Accept":     "application/json",
	}
	c := newClient(headers)
	var data struct {
		Data struct {
			Children []struct {
				Data struct {
					Title     string `json:"title"`
					URL       string `json:"url"`
					Score     int    `json:"score"`
					Subreddit string `json:"subreddit"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	resp, err := getJSON(c, "https://www.reddit.com/r/popular/.json", headers, map[string]string{"limit": strconv.Itoa(n)}, &data)
	if err != nil {
		// 限流时再试 old.reddit.com
		if resp != nil && resp.StatusCode == 429 {
			resp2, err2 := getJSON(c, "https://old.reddit.com/r/popular/.json", headers, map[string]string{"limit": strconv.Itoa(n)}, &data)
			_ = resp2
			err = err2
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Reddit] 错误: %v\n", err)
			return nil
		}
	}
	var items []Item
	for i, child := range data.Data.Children {
		if i >= n {
			break
		}
		p := child.Data
		u := p.URL
		if u == "" {
			u = "https://reddit.com"
		}
		items = append(items, Item{
			Source: "Reddit", Rank: i + 1, Title: p.Title, URL: u,
			Desc:  "r/" + p.Subreddit,
			Heat:  fmt.Sprintf("[+1] %d", p.Score),
			Extra: "r/" + p.Subreddit,
		})
	}
	return items
}

// fetchHackerNews Hacker News 热门 — Firebase 公开 API，无 Key
func fetchHackerNews(n int) []Item {
	c := newClient(nil)
	var ids []int
	if _, err := getJSON(c, "https://hacker-news.firebaseio.com/v0/topstories.json", nil, nil, &ids); err != nil {
		fmt.Fprintf(os.Stderr, "[HackerNews] 错误: %v\n", err)
		return nil
	}
	if n > len(ids) {
		n = len(ids)
	}
	type story struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		ID    int    `json:"id"`
		Score int    `json:"score"`
		By    string `json:"by"`
	}
	results := make([]story, n)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx, id int) {
			defer wg.Done()
			var st story
			_, err := getJSON(c, fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id), nil, nil, &st)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
			results[idx] = st
		}(i, ids[i])
	}
	wg.Wait()
	if firstErr != nil {
		fmt.Fprintf(os.Stderr, "[HackerNews] 错误: %v\n", firstErr)
	}
	var items []Item
	for i, st := range results {
		if st.Title == "" {
			continue
		}
		u := st.URL
		if u == "" {
			u = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", st.ID)
		}
		desc := ""
		if st.By != "" {
			desc = "by " + st.By
		}
		items = append(items, Item{
			Source: "HackerNews", Rank: i + 1, Title: st.Title, URL: u,
			Desc:  desc,
			Heat:  fmt.Sprintf("[^] %d", st.Score),
			Extra: "news.ycombinator.com",
		})
	}
	return items
}

// fetchV2EX V2EX 热门主题 — 公开 JSON API
func fetchV2EX(n int) []Item {
	c := newClient(nil)
	var data []struct {
		Title   string `json:"title"`
		ID      int    `json:"id"`
		Replies int    `json:"replies"`
		Node    struct {
			Title string `json:"title"`
		} `json:"node"`
		Member struct {
			Username string `json:"username"`
		} `json:"member"`
	}
	if _, err := getJSON(c, "https://www.v2ex.com/api/topics/hot.json", nil, nil, &data); err != nil {
		fmt.Fprintf(os.Stderr, "[V2EX] 错误: %v\n", err)
		return nil
	}
	var items []Item
	for i, t := range data {
		if i >= n {
			break
		}
		u := fmt.Sprintf("https://www.v2ex.com/t/%d", t.ID)
		node := t.Node.Title
		desc := t.Member.Username + " · " + node
		items = append(items, Item{
			Source: "V2EX", Rank: i + 1, Title: t.Title, URL: u,
			Desc:  desc,
			Heat:  fmt.Sprintf("[R] %d", t.Replies),
			Extra: node,
		})
	}
	return items
}

// fetchWeibo 微博热搜 — 移动端 JSON API
func fetchWeibo(n int) []Item {
	headers := map[string]string{
		"User-Agent": ua,
		"Referer":    "https://weibo.com/",
		"Accept":     "application/json, text/plain, */*",
	}
	c := newClient(headers)
	var data struct {
		Data struct {
			Realtime []struct {
				Word string      `json:"word"`
				Num  json.Number `json:"num"`
				Note string      `json:"note"`
			} `json:"realtime"`
		} `json:"data"`
	}
	if _, err := getJSON(c, "https://weibo.com/ajax/side/hotSearch", headers, nil, &data); err != nil {
		fmt.Fprintf(os.Stderr, "[微博] 错误: %v\n", err)
		return nil
	}
	var items []Item
	for i, e := range data.Data.Realtime {
		if i >= n {
			break
		}
		u := "https://s.weibo.com/weibo?q=" + e.Word
		heat := ""
		if e.Num.String() != "" {
			heat = "[H] " + e.Num.String()
		}
		items = append(items, Item{
			Source: "微博", Rank: i + 1, Title: e.Word, URL: u,
			Desc:  e.Note,
			Heat:  heat,
			Extra: "热搜",
		})
	}
	return items
}

// fetchZhihu 知乎热榜 — 从发现页解析热门问题
func fetchZhihu(n int) []Item {
	headers := map[string]string{
		"User-Agent": ua,
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}
	c := newClient(headers)
	resp, body, err := doGet(c, "https://www.zhihu.com/explore", headers, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[知乎] 错误: %v\n", err)
		return nil
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "[知乎] 错误: HTTP %d\n", resp.StatusCode)
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[知乎] 错误: %v\n", err)
		return nil
	}
	var items []Item
	seen := map[string]bool{}
	doc.Find(`a[href*='/question/']`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if len(items) >= n {
			return false
		}
		title := strings.TrimSpace(s.Text())
		href, _ := s.Attr("href")
		if len(title) < 6 || seen[title] {
			return true
		}
		seen[title] = true
		if strings.HasPrefix(href, "/") {
			href = "https://www.zhihu.com" + href
		}
		items = append(items, Item{
			Source: "知乎", Rank: len(items) + 1, Title: title, URL: href,
			Desc:  "",
			Heat:  "",
			Extra: "zhihu.com",
		})
		return true
	})
	return items
}

// ── 并发抓取所有源 ────────────────────────────────────────────────────

func fetchAll(n int) (github, reddit, zhihu, hackernews, v2ex, weibo []Item) {
	var wg sync.WaitGroup
	wg.Add(6)
	go func() { defer wg.Done(); github = fetchGitHub(n) }()
	go func() { defer wg.Done(); reddit = fetchReddit(n) }()
	go func() { defer wg.Done(); zhihu = fetchZhihu(n) }()
	go func() { defer wg.Done(); hackernews = fetchHackerNews(n) }()
	go func() { defer wg.Done(); v2ex = fetchV2EX(n) }()
	go func() { defer wg.Done(); weibo = fetchWeibo(n) }()
	wg.Wait()
	return
}

// termWidth 获取终端宽度；非 TTY（如管道）时返回 120 以保证可读输出
func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 120
}
