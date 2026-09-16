<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  Alert, Button, Card, Empty, Form, FormItem, Input, Modal, Popconfirm, Select, Space, Table, Tag, message,
} from 'ant-design-vue'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons-vue'
import { listHosts, saveHosts, type MosdnsHost } from '../api/mosdns'
import { restart } from '../api/process'

const [messageApi, contextHolder] = message.useMessage()
const hosts = ref<MosdnsHost[]>([])
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)
const modalOpen = ref(false)
const editingIndex = ref(-1)
const form = reactive<{ domain: string; ips: string[] }>({ domain: '', ips: [] })

const columns = [
  { title: '域名', dataIndex: 'domain', key: 'domain', width: 260 },
  { title: 'IP 地址', key: 'ips' },
  { title: '操作', key: 'action', width: 120 },
]

async function load() {
  loading.value = true
  try {
    hosts.value = await listHosts()
    dirty.value = false
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function openAdd() {
  editingIndex.value = -1
  form.domain = ''
  form.ips = []
  modalOpen.value = true
}

function openEdit(index: number) {
  editingIndex.value = index
  form.domain = hosts.value[index].domain
  form.ips = [...hosts.value[index].ips]
  modalOpen.value = true
}

function confirmEdit() {
  const domain = form.domain.trim()
  if (!domain) {
    messageApi.warning('域名不能为空')
    return
  }
  if (form.ips.length === 0) {
    messageApi.warning('至少填写一个 IP 地址')
    return
  }
  // 去重（保留顺序）。
  const ips = Array.from(new Set(form.ips.map((s) => s.trim()).filter(Boolean)))
  const row = { domain, ips }
  if (editingIndex.value >= 0) hosts.value.splice(editingIndex.value, 1, row)
  else hosts.value.push(row)
  dirty.value = true
  modalOpen.value = false
}

function remove(index: number) {
  hosts.value.splice(index, 1)
  dirty.value = true
}

async function save(restart: boolean) {
  saving.value = true
  try {
    hosts.value = await saveHosts(hosts.value)
    dirty.value = false
    if (restart) {
      await restart()
      messageApi.success('已保存并重启 mosdns')
    } else {
      messageApi.success('已保存（重启后生效）')
    }
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Card title="内网解析记录" size="small">
    <Alert
      type="info"
      show-icon
      message="按 mosdns hosts 规则把域名固定解析到指定 IP，内网访问已发布服务时不再绕行公网"
      style="margin-bottom: 16px"
    />

    <Space style="margin-bottom: 12px">
      <Button type="primary" @click="openAdd">
        <template #icon><PlusOutlined /></template>
        新增记录
      </Button>
      <Button :disabled="!dirty" :loading="saving" @click="save(true)">
        <template #icon><SaveOutlined /></template>
        保存并重启
      </Button>
      <Button :disabled="!dirty" :loading="saving" @click="save(false)">
        仅保存
      </Button>
      <Button :loading="loading" @click="load">
        <template #icon><ReloadOutlined /></template>
        刷新
      </Button>
      <span v-if="dirty" style="color: #faad14">有未保存的改动</span>
    </Space>

    <Table :columns="columns" :data-source="hosts" :loading="loading" size="middle" :pagination="false" row-key="domain">
      <template #bodyCell="{ column, record, index }">
        <template v-if="column.key === 'domain'">
          <span style="font-family: monospace">{{ record.domain }}</span>
        </template>
        <template v-else-if="column.key === 'ips'">
          <Tag v-for="ip in record.ips" :key="ip" color="blue" style="margin-bottom: 4px">{{ ip }}</Tag>
        </template>
        <template v-else-if="column.key === 'action'">
          <Space :size="4">
            <Button type="text" size="small" @click="openEdit(index)">
              <template #icon><EditOutlined /></template>
            </Button>
            <Popconfirm title="确定删除该记录？" @confirm="remove(index)">
              <Button type="text" size="small" danger>
                <template #icon><DeleteOutlined /></template>
              </Button>
            </Popconfirm>
          </Space>
        </template>
      </template>
      <template #emptyText>
        <Empty description="暂无内网解析记录" />
      </template>
    </Table>
  </Card>

  <Modal v-model:open="modalOpen" :title="editingIndex >= 0 ? '编辑解析记录' : '新增解析记录'" @ok="confirmEdit">
    <Form layout="vertical">
      <FormItem label="域名">
        <Input v-model:value="form.domain" placeholder="如 router.lan" />
      </FormItem>
      <FormItem label="IP 地址" help="可填多个，回车分隔；支持 IPv4 / IPv6">
        <Select
          v-model:value="form.ips"
          mode="tags"
          placeholder="如 192.168.1.1"
          :token-separators="[',', ' ']"
        />
      </FormItem>
    </Form>
  </Modal>
</template>
