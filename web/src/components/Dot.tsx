import './uikit.css'

export type DotState =
  | 'active'
  | 'ready'
  | 'idle'
  | 'blocked'
  | 'waiting_input'
  | 'errored'
  | 'exited'
  | 'spawning'

const toneClass: Record<DotState, string> = {
  active: 'dot--ok',
  ready: 'dot--ok',
  idle: 'dot--neutral',
  blocked: 'dot--warn',
  waiting_input: 'dot--warn',
  errored: 'dot--err',
  exited: 'dot--err',
  spawning: 'dot--indigo',
}

export interface DotProps {
  state: DotState
}

export function Dot({ state }: DotProps) {
  const classes = ['dot', toneClass[state]]
  if (state === 'active') classes.push('dot--pulse')
  return <span className={classes.join(' ')} />
}
