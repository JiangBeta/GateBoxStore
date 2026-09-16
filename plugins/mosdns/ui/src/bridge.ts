import { setToken } from './api/http'

// bootstrap 先取 plugin token（同源，内核提供 /token 端点），再挂载应用，
// 避免首屏请求因缺 token 而 401。
export async function bootstrap() {
  try {
    const r = await fetch('/api/v1/plugins/mosdns/token')
    if (r.ok) {
      const j = await r.json()
      setToken(j.token || '')
    }
  } catch {
    // 由宿主 init 消息兜底注入。
  }
}

// setupBridge 建立 postMessage 桥（宿主能力：ready/resize/navigate/toast/setTitle）。
export function setupBridge() {
  window.addEventListener('message', (e) => {
    const d = (e.data || {}) as Record<string, any>
    if (d.type === 'init' && d.token) setToken(String(d.token))
  })

  window.parent?.postMessage({ type: 'ready' }, '*')
  window.parent?.postMessage({ type: 'setTitle', title: 'MosDNS' }, '*')

  const report = () =>
    window.parent?.postMessage({ type: 'resize', height: document.documentElement.scrollHeight }, '*')
  try {
    new ResizeObserver(report).observe(document.body)
  } catch {
    window.setInterval(report, 1000)
  }
  report()
}
