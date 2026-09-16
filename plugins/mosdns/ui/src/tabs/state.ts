import { ref } from 'vue'

// 插件页内部 Tab 状态（宿主不感知；插件自带完整页面）。
export const activeTab = ref('status')
export function switchTab(t: string) {
  activeTab.value = t
}
