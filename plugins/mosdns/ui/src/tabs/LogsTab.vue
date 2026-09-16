<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { Button, Card, Input, Popconfirm, Select, Space, Switch, Tag, Typography, message } from 'ant-design-vue'
import { ClearOutlined, DownloadOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { clearLogs, wsURL, type LogMessage } from '../api/mosdns'

const [messageApi, contextHolder] = message.useMessage()

const MAX_LINES = 5000
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'] as const
type Level = (typeof LEVELS)[number]
const levelRank: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 }
const levelOptions = LEVELS.map((l) => ({ value: l, label: l }))

const lines = ref<string[]>([])
const keyword = ref('')
const level = ref<Level>('INFO')
const follow = ref(true)
const connected = ref(false)
const errorMsg = ref('')
const truncated = ref(false)
const clearing = ref(false)
const bodyRef = ref<HTMLElement | null>(null)
let ws: WebSocket | null = null

/** 从文本行识别日志等级；未知按 INFO 处理。 */
function lineRank(line: string): number {
  const m = line.toUpperCase().match(/\b(DEBUG|INFO|WARN|ERROR)\b/)
  return m ? levelRank[m[1]] ?? 1 : 1
}

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const min = levelRank[level.value]
  return lines.value.filter((l) => lineRank(l) >= min && (!k || l.toLowerCase().includes(k)))
})

function connect() {
  ws = new WebSocket(wsURL('/mosdns/logs/stream', { follow: 'true', tail: '500' }))
  ws.onopen = () => {
    connected.value = true
    errorMsg.value = ''
  }
  ws.onmessage = (ev) => {
    let msg: LogMessage
    try {
      msg = JSON.parse(ev.data)
    } catch {
      return
    }
    if (msg.error) {
      errorMsg.value = msg.error
      return
    }
    if (msg.data) {
      lines.value.push(msg.data.replace(/\n$/, ''))
      if (lines.value.length > MAX_LINES) {
        lines.value = lines.value.slice(-MAX_LINES)
        truncated.value = true
      }
      if (follow.value) scrollToBottom()
    }
  }
  ws.onclose = () => {
    connected.value = false
  }
}

function scrollToBottom() {
  nextTick(() => {
    const el = bodyRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

async function onClear() {
  clearing.value = true
  try {
    await clearLogs()
    lines.value = []
    messageApi.success('日志已清空')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    clearing.value = false
  }
}

function download() {
  const blob = new Blob([lines.value.join('\n')], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'mosdns.log'
  a.click()
  URL.revokeObjectURL(a.href)
}

onMounted(connect)
onUnmounted(() => {
  ws?.close()
  ws = null
})
</script>

<template>
  <contextHolder />
  <Card title="运行日志" size="small">
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px">
      <Space align="center" :size="8" wrap>
        <Select v-model:value="level" size="small" style="width: 96px" :options="levelOptions" />
        <Input v-model:value="keyword" placeholder="查找日志" allow-clear style="width: 200px" size="small" />
        <span style="font-size: 13px; color: #666">
          {{ filtered.length }} / {{ lines.length }} 行
          <span v-if="truncated">（已截断至最近 {{ MAX_LINES }} 行）</span>
        </span>
        <Space align="center" :size="6">
          <span style="font-size: 13px; color: #666">自动滚动</span>
          <Switch v-model:checked="follow" size="small" @change="follow && scrollToBottom()" />
        </Space>
        <Button size="small" @click="download">
          <template #icon><DownloadOutlined /></template>
          下载
        </Button>
        <Popconfirm title="确定清空日志？" @confirm="onClear">
          <Button size="small" danger :loading="clearing">
            <template #icon><ClearOutlined /></template>
            清空
          </Button>
        </Popconfirm>
        <Button size="small" @click="connect">
          <template #icon><ReloadOutlined /></template>
          重连
        </Button>
      </Space>
      <Tag :color="connected ? 'success' : 'default'" size="small">{{ connected ? '已连接' : '已断开' }}</Tag>
    </div>
    <div v-if="errorMsg" style="color: #ff4d4f; font-size: 12px; margin-bottom: 8px">{{ errorMsg }}</div>

    <div ref="bodyRef" class="log-body">
      <div v-if="!filtered.length" class="log-empty">
        <Typography.Text type="secondary">{{ lines.length ? '没有匹配的日志行' : '暂无日志' }}</Typography.Text>
      </div>
      <div v-for="(l, i) in filtered" :key="i" class="log-line">{{ l }}</div>
    </div>
  </Card>
</template>

<style scoped>
.log-body {
  height: 520px;
  overflow: auto;
  background: #1e1e1e;
  color: #d4d4d4;
  border-radius: 4px;
  padding: 10px 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.55;
}
.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
.log-empty {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}
</style>
