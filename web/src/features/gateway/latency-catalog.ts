export type LatencyCategory = 'communication' | 'social' | 'entertainment' | 'news' | 'shopping' | 'tools'

export interface CatalogSite {
  name: string
  category: LatencyCategory
  url: string
}

export const LATENCY_GROUPS: { key: LatencyCategory; label: string }[] = [
  { key: 'communication', label: 'Communication' },
  { key: 'social', label: 'Social' },
  { key: 'entertainment', label: 'Entertainment' },
  { key: 'news', label: 'News' },
  { key: 'shopping', label: 'Shopping' },
  { key: 'tools', label: 'Tools' },
]

// One address per site, chosen from measurements on the reference router: for
// each app, the faster of its main page and a static or CDN host. A CDN root
// can answer 403/404, which the probe still counts — the metric is the edge's
// response time, not the object. Sites are added one at a time; the operator
// still chooses an exit per site.
export const LATENCY_CATALOG: CatalogSite[] = [
  { name: 'WeChat', category: 'communication', url: 'https://res.wx.qq.com/' },
  { name: 'QQ', category: 'communication', url: 'https://qq.com/' },
  { name: 'Telegram', category: 'communication', url: 'https://t.me/' },
  { name: 'Feishu', category: 'communication', url: 'https://sf3-scmcdn-cn.feishucdn.com/' },

  { name: 'Weibo', category: 'social', url: 'https://weibo.com/' },
  { name: 'Xiaohongshu', category: 'social', url: 'https://sns-img-qc.xhscdn.com/' },
  { name: 'X', category: 'social', url: 'https://x.com/' },
  { name: 'Douban', category: 'social', url: 'https://www.douban.com/' },

  { name: 'Douyin', category: 'entertainment', url: 'https://p3-pc-sign.douyinpic.com/' },
  { name: 'Bilibili', category: 'entertainment', url: 'https://i0.hdslb.com/' },
  { name: 'YouTube', category: 'entertainment', url: 'https://www.youtube.com/' },
  { name: 'Netflix', category: 'entertainment', url: 'https://assets.nflxext.com/' },
  { name: 'Spotify', category: 'entertainment', url: 'https://i.scdn.co/' },

  { name: 'Sina News', category: 'news', url: 'https://news.sina.com.cn/' },
  { name: 'Tencent News', category: 'news', url: 'https://news.qq.com/' },
  { name: 'NetEase News', category: 'news', url: 'https://news.163.com/' },
  { name: 'CNN', category: 'news', url: 'https://www.cnn.com/' },

  { name: 'Taobao', category: 'shopping', url: 'https://www.taobao.com/' },
  { name: 'JD', category: 'shopping', url: 'https://www.jd.com/' },
  { name: 'Pinduoduo', category: 'shopping', url: 'https://img.pddpic.com/' },
  { name: 'Amazon', category: 'shopping', url: 'https://m.media-amazon.com/' },

  { name: 'Baidu', category: 'tools', url: 'https://www.baidu.com/' },
  { name: 'Google', category: 'tools', url: 'https://www.gstatic.com/' },
  { name: 'GitHub', category: 'tools', url: 'https://github.com/' },
  { name: 'Zhihu', category: 'tools', url: 'https://pic1.zhimg.com/' },
]
