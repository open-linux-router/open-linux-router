import type { SankeySeriesOption } from 'echarts/charts'

/** Unit weights are layout weights, never packet counters or percentages. */
export function sankeyOption(width: number, height: number, colors: string[], outgoing = false): SankeySeriesOption {
  const ribbon = Math.min(28, width * 0.055)
  const outer = outgoing ? [0.27, 0.73] : [0.18, 0.5, 0.82]
  const inner = outgoing ? [0.42, 0.58] : [0.4, 0.5, 0.6]
  return {
    type: 'sankey',
    orient: 'vertical',
    left: 0,
    top: 0,
    width,
    height,
    nodeWidth: 0,
    nodeGap: (width - colors.length * ribbon) / (colors.length - 1),
    layoutIterations: 0,
    draggable: false,
    silent: true,
    label: { show: false },
    itemStyle: { opacity: 0 },
    data: colors.flatMap((_, i) => [
      { name: `start-${i}`, localX: (outgoing ? inner[i] : outer[i]) - ribbon / width / 2, localY: 0 },
      { name: `end-${i}`, localX: (outgoing ? outer[i] : inner[i]) - ribbon / width / 2, localY: 1 },
    ]),
    links: colors.map((color, i) => ({
      source: `start-${i}`,
      target: `end-${i}`,
      value: 1,
      lineStyle: { color, opacity: 0.5, curveness: 0.5 },
    })),
  }
}
