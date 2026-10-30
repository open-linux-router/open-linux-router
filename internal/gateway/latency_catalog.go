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
	Icon     string `json:"icon,omitempty"`
}

// CommonLatency is the operator's per-site override for a common site. Only
// the exit is configurable: the name, URL, category and icon come from the catalog.
type CommonLatency struct {
	Name string `json:"name"`
	Exit string `json:"exit,omitempty"`
}

func latencyCatalog() []CommonLatencySite {
	return []CommonLatencySite{
		{Name: "WeChat", URL: "https://res.wx.qq.com/", Category: "communication", Icon: "thesvg:wechat"},
		{Name: "QQ", URL: "https://qq.com/", Category: "communication", Icon: "thesvg:qq"},
		{Name: "Telegram", URL: "https://t.me/", Category: "communication", Icon: "thesvg:telegram"},
		{Name: "Feishu", URL: "https://sf3-scmcdn-cn.feishucdn.com/", Category: "communication"},

		{Name: "Weibo", URL: "https://weibo.com/", Category: "social", Icon: "thesvg:sina-weibo"},
		{Name: "Xiaohongshu", URL: "https://sns-img-qc.xhscdn.com/", Category: "social", Icon: "thesvg:xiaohongshu"},
		{Name: "X", URL: "https://x.com/", Category: "social", Icon: "thesvg:x"},
		{Name: "Douban", URL: "https://www.douban.com/", Category: "social", Icon: "thesvg:douban"},

		{Name: "Douyin", URL: "https://p3-pc-sign.douyinpic.com/", Category: "entertainment"},
		{Name: "Bilibili", URL: "https://i0.hdslb.com/", Category: "entertainment", Icon: "thesvg:bilibili"},
		{Name: "YouTube", URL: "https://www.youtube.com/", Category: "entertainment", Icon: "thesvg:youtube"},
		{Name: "Netflix", URL: "https://assets.nflxext.com/", Category: "entertainment", Icon: "thesvg:netflix"},
		{Name: "Spotify", URL: "https://i.scdn.co/", Category: "entertainment", Icon: "thesvg:spotify"},

		{Name: "Sina News", URL: "https://news.sina.com.cn/", Category: "news"},
		{Name: "Tencent News", URL: "https://news.qq.com/", Category: "news"},
		{Name: "NetEase News", URL: "https://news.163.com/", Category: "news"},
		{Name: "CNN", URL: "https://www.cnn.com/", Category: "news", Icon: "thesvg:cnn"},

		{Name: "Taobao", URL: "https://www.taobao.com/", Category: "shopping", Icon: "thesvg:taobao"},
		{Name: "JD", URL: "https://www.jd.com/", Category: "shopping"},
		{Name: "Pinduoduo", URL: "https://img.pddpic.com/", Category: "shopping"},
		{Name: "Amazon", URL: "https://m.media-amazon.com/", Category: "shopping", Icon: "thesvg:amazon"},

		{Name: "Baidu", URL: "https://www.baidu.com/", Category: "tools", Icon: "thesvg:baidu"},
		{Name: "Google", URL: "https://www.gstatic.com/", Category: "tools", Icon: "thesvg:google"},
		{Name: "GitHub", URL: "https://github.com/", Category: "tools", Icon: "thesvg:github"},
		{Name: "Zhihu", URL: "https://pic1.zhimg.com/", Category: "tools", Icon: "thesvg:zhihu"},
	}
}

func validLatencyCategory(category string) bool {
	if category == "" || category == "custom" {
		return true
	}
	for _, site := range latencyCatalog() {
		if site.Category == category {
			return true
		}
	}
	return false
}

func commonLatencySite(name string) (CommonLatencySite, bool) {
	for _, site := range latencyCatalog() {
		if site.Name == name {
			return site, true
		}
	}
	return CommonLatencySite{}, false
}
