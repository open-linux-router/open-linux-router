package gateway

// Common sites are part of the product, not the operator's document: they are
// always present and always probed, so the latency page works on a fresh
// install. The operator only chooses an exit for one; that choice lives in
// gateway.latency_common. URLs were chosen from measurements on the reference
// router — for each app, the faster of its main page and a static or CDN host.
// A CDN root can answer 403/404, which the probe still counts: the metric is
// the edge's response time, not the object.
type CommonLatencySite struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category"`
}

// CommonLatency is the operator's per-site override for a common site. Only
// the exit is configurable: the name, URL and category come from the catalog.
type CommonLatency struct {
	Name string `json:"name"`
	Exit string `json:"exit,omitempty"`
}

func latencyCatalog() []CommonLatencySite {
	return []CommonLatencySite{
		{Name: "WeChat", URL: "https://res.wx.qq.com/", Category: "communication"},
		{Name: "QQ", URL: "https://qq.com/", Category: "communication"},
		{Name: "Telegram", URL: "https://t.me/", Category: "communication"},
		{Name: "Feishu", URL: "https://sf3-scmcdn-cn.feishucdn.com/", Category: "communication"},

		{Name: "Weibo", URL: "https://weibo.com/", Category: "social"},
		{Name: "Xiaohongshu", URL: "https://sns-img-qc.xhscdn.com/", Category: "social"},
		{Name: "X", URL: "https://x.com/", Category: "social"},
		{Name: "Douban", URL: "https://www.douban.com/", Category: "social"},

		{Name: "Douyin", URL: "https://p3-pc-sign.douyinpic.com/", Category: "entertainment"},
		{Name: "Bilibili", URL: "https://i0.hdslb.com/", Category: "entertainment"},
		{Name: "YouTube", URL: "https://www.youtube.com/", Category: "entertainment"},
		{Name: "Netflix", URL: "https://assets.nflxext.com/", Category: "entertainment"},
		{Name: "Spotify", URL: "https://i.scdn.co/", Category: "entertainment"},

		{Name: "Sina News", URL: "https://news.sina.com.cn/", Category: "news"},
		{Name: "Tencent News", URL: "https://news.qq.com/", Category: "news"},
		{Name: "NetEase News", URL: "https://news.163.com/", Category: "news"},
		{Name: "CNN", URL: "https://www.cnn.com/", Category: "news"},

		{Name: "Taobao", URL: "https://www.taobao.com/", Category: "shopping"},
		{Name: "JD", URL: "https://www.jd.com/", Category: "shopping"},
		{Name: "Pinduoduo", URL: "https://img.pddpic.com/", Category: "shopping"},
		{Name: "Amazon", URL: "https://m.media-amazon.com/", Category: "shopping"},

		{Name: "Baidu", URL: "https://www.baidu.com/", Category: "tools"},
		{Name: "Google", URL: "https://www.gstatic.com/", Category: "tools"},
		{Name: "GitHub", URL: "https://github.com/", Category: "tools"},
		{Name: "Zhihu", URL: "https://pic1.zhimg.com/", Category: "tools"},
	}
}

func commonLatencySite(name string) (CommonLatencySite, bool) {
	for _, site := range latencyCatalog() {
		if site.Name == name {
			return site, true
		}
	}
	return CommonLatencySite{}, false
}
