import { useEffect, useRef } from 'react'
import { SankeyChart } from 'echarts/charts'
import { init, use as registerCharts } from 'echarts/core'
import { SVGRenderer } from 'echarts/renderers'
import { sankeyOption } from './sankey-option'

registerCharts([SankeyChart, SVGRenderer])

/** Equal visual weights: the API does not measure all accepted categories. */
export default function Sankey({ colors, outgoing = false }: { colors: string[]; outgoing?: boolean }) {
  const host = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!host.current) return
    const chart = init(host.current, undefined, { renderer: 'svg' })
    const render = () => {
      chart.resize()
      const width = chart.getWidth()
      const height = chart.getHeight()
      if (width <= 0 || height <= 0) return
      chart.setOption({ animation: false, series: [sankeyOption(width, height, colors, outgoing)] })
    }
    const observer = new ResizeObserver(render)
    observer.observe(host.current)
    render()
    return () => {
      observer.disconnect()
      chart.dispose()
    }
  }, [colors, outgoing])

  return <div ref={host} aria-hidden="true" className="h-full w-full" />
}
