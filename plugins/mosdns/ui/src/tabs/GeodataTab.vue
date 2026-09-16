<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Alert, Button, Card, Input, Space, Table, Tag, message } from 'ant-design-vue'
import { ReloadOutlined, CloudDownloadOutlined } from '@ant-design/icons-vue'
import {
  getSettings, listGeodata, saveSettings, updateGeodata, type GeoItem,
} from '../api/mosdns'

const [messageApi, contextHolder] = message.useMessage()
const items = ref<GeoItem[]>([])
const geoProxy = ref('')
const loading = ref(false)
const updating = ref(false)

const columns = [
  { title: '数据', key: 'label', width: 200 },
  { title: '文件', dataIndex: 'name', key: 'name', width: 240 },
  { title: '大小', key: 'size', width: 120 },
  { title: '更新时间', key: 'updatedAt', width: 200 },
  { title: '来源', key: 'url' },
]

function fmtSize(n?: number): string {
  if (!n) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

function fmtTime(s?: string): string {
  if (!s) return '—'
  return s.replace('T', ' ').slice(0, 19)
}

async function load() {
  loading.value = true
  try {
    items.value = await listGeodata()
    geoProxy.value = (await getSettings()).geoProxy || ''
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function update() {
  updating.value = true
  try {
    const s = await getSettings()
    // 仅在代理变化时保存设置，避免无谓地依默认重新生成 config.yaml。
    if ((s.geoProxy || '') !== geoProxy.value.trim()) {
      await saveSettings({ ...s, geoProxy: geoProxy.value.trim() })
    }
    const { results, items: list } = await updateGeodata()
    items.value = list
    const failed = results.filter((r) => r.error)
    if (failed.length) {
      messageApi.warning(`${failed.length} 个文件更新失败：${failed[0].name}（${failed[0].error}）`)
    } else {
      messageApi.success('数据库更新完成')
    }
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    updating.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Card title="数据库更新" size="small">
    <Alert
      type="info"
      show-icon
      message="更新 GeoIP / GeoSite 数据与规则库"
      description="下载社区维护的纯文本域名 / IP 列表（国内域名、国外域名、Apple 域名、国内 IP），覆盖 tools/mosdns 下对应文件。更新后点「状态 → 重启」生效。"
      style="margin-bottom: 16px"
    />

    <Space style="margin-bottom: 12px" wrap>
      <Button type="primary" :loading="updating" @click="update">
        <template #icon><CloudDownloadOutlined /></template>
        更新数据库
      </Button>
      <Button :loading="loading" :disabled="updating" @click="load">
        <template #icon><ReloadOutlined /></template>
        刷新
      </Button>
      <span style="color: #888">GitHub 代理</span>
      <Input v-model:value="geoProxy" placeholder="留空直连，如 https://gh-proxy.com/" style="width: 320px" allow-clear />
      <Tag v-if="updating" color="processing">更新中，可能耗时数分钟…</Tag>
    </Space>

    <Table :columns="columns" :data-source="items" :loading="loading" size="middle" :pagination="false" row-key="name">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'label'">
          <Tag color="blue">{{ record.label }}</Tag>
        </template>
        <template v-else-if="column.key === 'name'">
          <span style="font-family: monospace">{{ record.name }}</span>
        </template>
        <template v-else-if="column.key === 'size'">{{ fmtSize(record.size) }}</template>
        <template v-else-if="column.key === 'updatedAt'">{{ fmtTime(record.updatedAt) }}</template>
        <template v-else-if="column.key === 'url'">
          <span style="font-family: monospace; font-size: 12px; color: #888">{{ record.url }}</span>
        </template>
      </template>
    </Table>
  </Card>
</template>
