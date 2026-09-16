import http from './http'

/** mosdns 运行状态（进程态 + 配置解析出的路径/端口）。 */
export interface MosdnsStatus {
  installed: boolean
  state: string
  healthy: boolean
  version: string
  latest: string
  updateAvailable: boolean
  listen: string
  apiAddr: string
  configPath: string
  configExists: boolean
  hostsPath: string
  logFile: string
  cacheTag: string
}

/** 基础/高级/Cloudflare 设置（对应后端 mosdns.Settings）。 */
export interface MosdnsSettings {
  listen: string
  logLevel: string
  localDns: string[]
  remoteDns: string[]
  streamDns: string[]
  bootstrap: string
  concurrent: number
  idleTimeout: number
  enablePipeline: boolean
  insecureSkipVerify: boolean
  enableEcsRemote: boolean
  remoteEcsIp: string
  dnsLeak: boolean
  preferIpv4Cn: boolean
  preferIpv4: boolean
  appleOptimization: boolean
  customStreamMediaDns: boolean
  cache: boolean
  cacheSize: number
  lazyCacheTtl: number
  dumpFile: boolean
  dumpInterval: number
  minimalTtl: number
  maximumTtl: number
  rejectType65: boolean
  adblock: boolean
  adSources: string[]
  cloudflare: boolean
  cloudflareIp: string[]
  geoProxy: string
}

/** 一条内网解析记录。 */
export interface MosdnsHost {
  domain: string
  ips: string[]
}

export interface MosdnsConfig {
  content: string
  path: string
  configured: boolean
}

/** 规则列表描述。 */
export interface RuleMeta {
  name: string
  label: string
  help: string
}

/** 数据库文件状态。 */
export interface GeoItem {
  name: string
  label: string
  url: string
  size: number
  updatedAt?: string
}

export interface GeoResult {
  name: string
  label?: string
  size?: number
  error?: string
}

export const DEFAULT_SETTINGS: MosdnsSettings = {
  listen: '0.0.0.0:5335',
  logLevel: 'error',
  localDns: ['223.5.5.5', '119.29.29.29'],
  remoteDns: ['tls://8.8.8.8', 'tls://1.1.1.1'],
  streamDns: ['tls://8.8.8.8'],
  bootstrap: '119.29.29.29',
  concurrent: 2,
  idleTimeout: 30,
  enablePipeline: true,
  insecureSkipVerify: false,
  enableEcsRemote: false,
  remoteEcsIp: '',
  dnsLeak: false,
  preferIpv4Cn: false,
  preferIpv4: false,
  appleOptimization: false,
  customStreamMediaDns: false,
  cache: true,
  cacheSize: 8000,
  lazyCacheTtl: 86400,
  dumpFile: false,
  dumpInterval: 3600,
  minimalTtl: 0,
  maximumTtl: 0,
  rejectType65: false,
  adblock: false,
  adSources: [],
  cloudflare: false,
  cloudflareIp: [],
  geoProxy: '',
}

export async function getStatus(): Promise<MosdnsStatus> {
  return (await http.get('/status')).data
}

export async function getSettings(): Promise<MosdnsSettings> {
  return (await http.get('/settings')).data
}

/** 保存设置并重新生成 config.yaml（会覆盖手工编辑）。 */
export async function saveSettings(s: MosdnsSettings): Promise<MosdnsSettings> {
  return (await http.put('/settings', s)).data
}

export async function getConfig(): Promise<MosdnsConfig> {
  return (await http.get('/config')).data
}

export async function saveConfig(content: string): Promise<{ path: string }> {
  return (await http.put('/config', { content })).data
}

export async function listHosts(): Promise<MosdnsHost[]> {
  return (await http.get('/hosts')).data
}

export async function saveHosts(hosts: MosdnsHost[]): Promise<MosdnsHost[]> {
  return (await http.put('/hosts', { hosts })).data
}

export async function listRules(): Promise<RuleMeta[]> {
  return (await http.get('/rules')).data
}

export async function getRule(name: string): Promise<{ name: string; content: string; path: string }> {
  return (await http.get(`/rules/${encodeURIComponent(name)}`)).data
}

export async function saveRule(name: string, content: string): Promise<{ name: string; path: string }> {
  return (await http.put(`/rules/${encodeURIComponent(name)}`, { content })).data
}

export async function listGeodata(): Promise<GeoItem[]> {
  return (await http.get('/geodata')).data
}

/** 更新数据库（耗时较长，放宽超时）。 */
export async function updateGeodata(): Promise<{ results: GeoResult[]; items: GeoItem[] }> {
  return (await http.post('/geodata/update', {}, { timeout: 900000 })).data
}

/** 下载广告规则来源（耗时较长，放宽超时）。 */
export async function updateAdblock(): Promise<{ results: GeoResult[] }> {
  return (await http.post('/adblock/update', {}, { timeout: 900000 })).data
}

export async function getLogs(): Promise<{ content: string; path: string }> {
  return (await http.get('/logs')).data
}

export async function clearLogs(): Promise<void> {
  await http.delete('/logs')
}

export async function flushCache(): Promise<void> {
  await http.post('/flush')
}

/** 构造 WebSocket 地址（开发模式经 vite 代理，生产为同源）。 */
export function wsURL(path: string, params: Record<string, string> = {}): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const qs = new URLSearchParams(params).toString()
  return `${proto}//${location.host}/api/v1/plugins/mosdns${path}${qs ? '?' + qs : ''}`
}

export interface LogMessage {
  stream?: string
  data?: string
  error?: string
}
