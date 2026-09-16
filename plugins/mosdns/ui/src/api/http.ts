import axios from 'axios'

// 插件后端基址：内核反代 /api/v1/plugins/mosdns/* → 本插件 sidecar（ADR-039）。
const http = axios.create({
  baseURL: '/api/v1/plugins/mosdns',
  timeout: 15000,
})

http.interceptors.response.use(
  (resp) => resp,
  (err) => {
    const raw = err?.response?.data?.error
    const msg = typeof raw === 'string' ? raw : raw?.message || err.message || '请求失败'
    return Promise.reject(new Error(msg))
  },
)

// setToken 注入 plugin token（宿主 init 或 /token 端点）。
export function setToken(t: string) {
  if (t) http.defaults.headers.common['X-Plugin-Token'] = t
}

export default http
