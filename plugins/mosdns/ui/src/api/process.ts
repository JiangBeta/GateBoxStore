import http from './http'

// mosdns 本体进程控制（由本插件 sidecar 代管，ADR-037 决策 (a)）。
export const start = () => http.post('/start').then((r) => r.data)
export const stop = () => http.post('/stop').then((r) => r.data)
export const restart = () => http.post('/restart').then((r) => r.data)
