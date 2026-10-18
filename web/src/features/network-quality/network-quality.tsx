import { Heading } from '@/components/ui/heading'
// Fixed sample scores keep the UI preview stable while live measurements are pending.
const sample = [
  { label: 'Speed', score: 86 },
  { label: 'Latency', score: 92 },
  { label: 'Stability', score: 78 },
  { label: 'Reachability', score: 95 },
  { label: 'Wi-Fi', score: 68 },
]

function point(index: number, score: number, radius = 86) {
  const angle = -Math.PI / 2 + index * Math.PI * 2 / sample.length
  return [150 + Math.cos(angle) * radius * score / 100, 116 + Math.sin(angle) * radius * score / 100]
}

function polygon(score: number) {
  return sample.map((_, index) => point(index, score).join(',')).join(' ')
}

export function NetworkQuality() {
  return <section aria-label="Quality" className="flex w-full min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:min-h-60 lg:aspect-[4/3] lg:h-fit">
    <div className="flex h-6 items-center justify-between gap-2">
      <Heading size="sm">Quality</Heading>
    </div>
    <div className="-mx-1.5 -mb-1.5 flex min-h-0 min-w-0 flex-1 items-center justify-center sm:-mx-2 sm:-mb-2">
      <figure className="flex h-full w-full flex-col items-center">
        <svg viewBox="0 10 340 215" className="min-h-0 w-full flex-1" role="img" aria-label={`Simulated network quality scores out of 100: ${sample.map(({ label, score }) => `${label} ${score}`).join(', ')}. Higher is better.`}>
          <g transform="translate(20 0)">
          {[25, 50, 75, 100].map(score => <polygon key={score} points={polygon(score)} className="fill-none stroke-border" strokeWidth="1" />)}
          {sample.map((item, index) => {
            const [x, y] = point(index, 100)
            return <line key={item.label} x1="150" y1="116" x2={x} y2={y} className="stroke-border" />
          })}
          <polygon points={sample.map((item, index) => point(index, item.score).join(',')).join(' ')} className="fill-primary/15 stroke-primary" strokeWidth="2" strokeLinejoin="round" />
          {sample.map((item, index) => {
            const [x, y] = point(index, item.score)
            const [labelX, labelY] = point(index, 100, 108)
            return <g key={item.label}>
              <circle cx={x} cy={y} r="3" className="fill-primary stroke-card" strokeWidth="1.5" />
              <text x={labelX} y={labelY} textAnchor="middle" className="fill-muted-foreground text-[15px]">{item.label} <tspan className="fill-foreground font-semibold tabular-nums">{item.score}</tspan></text>
            </g>
          })}
          </g>
        </svg>
      </figure>
    </div>
  </section>
}
