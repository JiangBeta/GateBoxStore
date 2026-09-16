<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  Alert, Button, Card, Descriptions, DescriptionsItem, Space, Tag, message,
} from 'ant-design-vue'
import {
  ClearOutlined, PauseCircleOutlined, PlayCircleOutlined, ReloadOutlined, SyncOutlined,
} from '@ant-design/icons-vue'
import { flushCache, getStatus, type MosdnsStatus } from '../api/mosdns'
import { start, stop, restart } from '../api/process'
import { switchTab } from './state'

const [messageApi, contextHolder] = message.useMessage()
const status = ref<MosdnsStatus | null>(null)
const busy = ref(false)
let timer: number | undefined

const running = computed(() => status.value?.state === 'running')
const installed = computed(() => status.value?.installed ?? false)
const configured = computed(() => status.value?.configExists ?? false)
const canStart = computed(() => installed.value && configured.value)

async function load() {
  try {
    status.value = await getStatus()
  } catch (e) {
    messageApi.error((e as Error).message)
  }
}

async function run(fn: () => Promise<unknown>, ok: string) {
  busy.value = true
  try {
    await fn()
    messageApi.success(ok)
    await load()
    // 通知宿主刷新组件/插件菜单（经 postMessage 桥）。
    window.parent?.postMessage({ type: 'toast', message: ok }, '*')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = false
  }
}

async function onFlush() {
  busy.value = true
  try {
    await flushCache()
    messageApi.success('缓存已刷新')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = false
  }
}

function goExtensions() {
  window.parent?.postMessage({ type: 'navigate', path: '/extensions' }, '*')
}

function stateColor(s?: string) {
  if (s === 'running') return 'green'
  if (s === 'error') return 'red'
  return 'default'
}

const stateText = computed(() => {
  if (!installed.value) return '未安装'
  switch (status.value?.state) {
    case 'running': return '运行中'
    case 'stopped': return '已停止'
    case 'error': return '有故障'
    default: return '未知'
  }
})

onMounted(() => {
  void load()
  timer = window.setInterval(load, 5000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <contextHolder />
  <Alert
    v-if="status && !installed"
    type="warning"
    show-icon
    message="mosdns 尚未安装"
    description="请先在「扩展 → 组件」中安装 mosdns 制品，随后回到本页生成配置并启动。"
    style="margin-bottom: 16px"
  >
    <template #action>
      <Button size="small" @click="goExtensions">前往扩展</Button>
    </template>
  </Alert>

  <Alert
    v-else-if="status && installed && !configured"
    type="warning"
    show-icon
    message="尚未生成 mosdns 配置"
    description="请先在「基础设置」保存一次（或「配置文件 → 生成默认配置」）再启动。"
    style="margin-bottom: 16px"
  >
    <template #action>
      <Button size="small" @click="switchTab('basic')">前往基础设置</Button>
    </template>
  </Alert>

  <Card title="运行状态" size="small">
    <template #extra>
      <Tag :color="stateColor(status?.state)">{{ stateText }}</Tag>
    </template>
    <Descriptions :column="2" size="small" bordered>
      <DescriptionsItem label="版本">
        {{ status?.version || '—' }}
        <Tag v-if="status?.updateAvailable" color="red" style="margin-left: 6px">可升级到 {{ status.latest }}</Tag>
      </DescriptionsItem>
      <DescriptionsItem label="监听地址">{{ status?.listen || '—' }}</DescriptionsItem>
      <DescriptionsItem label="API 地址">{{ status?.apiAddr || '—' }}</DescriptionsItem>
      <DescriptionsItem label="缓存插件">{{ status?.cacheTag || '未启用' }}</DescriptionsItem>
      <DescriptionsItem label="配置文件" :span="2">{{ status?.configPath || '—' }}</DescriptionsItem>
      <DescriptionsItem label="解析记录" :span="2">{{ status?.hostsPath || '—' }}</DescriptionsItem>
      <DescriptionsItem label="日志文件" :span="2">{{ status?.logFile || '—' }}</DescriptionsItem>
    </Descriptions>

    <Space style="margin-top: 16px" wrap>
      <Button
        v-if="!running"
        type="primary"
        :disabled="!canStart"
        :loading="busy"
        @click="run(() => start(), 'mosdns 已启动')"
      >
        <template #icon><PlayCircleOutlined /></template>
        启动
      </Button>
      <Button
        v-else
        :disabled="!installed"
        :loading="busy"
        @click="run(() => stop(), 'mosdns 已停止')"
      >
        <template #icon><PauseCircleOutlined /></template>
        停止
      </Button>
      <Button :disabled="!installed" :loading="busy" @click="run(() => restart(), 'mosdns 已重启')">
        <template #icon><ReloadOutlined /></template>
        重启
      </Button>
      <Button :disabled="!status?.cacheTag" :loading="busy" @click="onFlush">
        <template #icon><ClearOutlined /></template>
        刷新缓存
      </Button>
      <Button @click="load">
        <template #icon><SyncOutlined /></template>
        刷新
      </Button>
    </Space>
  </Card>
</template>
