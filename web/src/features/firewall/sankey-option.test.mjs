import assert from 'node:assert/strict'
import { test } from 'node:test'
import { init, use as registerCharts } from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { SVGRenderer } from 'echarts/renderers'
import { sankeyOption } from './sankey-option.ts'

registerCharts([SankeyChart, SVGRenderer])

for (const width of [280, 480, 896]) {
  for (const outgoing of [false, true]) {
    test(`${outgoing ? 'outbound' : 'inbound'} Sankey stays within ${width}px and fans at the wall`, () => {
      const colors = outgoing ? ['#a8afb7', '#78a9da'] : ['#8cb5a1', '#78a9da', '#d5a066']
      const option = sankeyOption(width, 176, colors, outgoing)
      assert.equal(option.orient, 'vertical')
      assert.ok(option.links.every((link) => link.value === 1))
      assert.ok(option.data.every((node) => node.localX >= 0 && node.localX < 1))
      const chart = init(null, null, { renderer: 'svg', ssr: true, width, height: 176 })
      try {
        chart.setOption({ animation: false, series: [option] })
        const svg = chart.renderToSVGString()
        assert.doesNotMatch(svg, /NaN|Infinity/)
        const ribbons = [...svg.matchAll(/<path d="([^"]+)" fill="([^"]+)"/g)].filter((match) => colors.includes(match[2]))
        assert.equal(ribbons.length, colors.length)
        const offsets = outgoing ? [0.42, 0.58] : [0.18, 0.5, 0.82]
        for (const [index, match] of ribbons.entries()) {
          const [x, y] = match[1].match(/^M([\d.]+) ([\d.]+)/).slice(1).map(Number)
          const ribbonWidth = Math.min(28, width * 0.055)
          assert.ok(Math.abs(x + ribbonWidth / 2 - width * offsets[index]) < 0.1)
          assert.equal(y, 0)
        }
      } finally {
        chart.dispose()
      }
    })
  }
}
