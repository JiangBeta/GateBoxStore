<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Alert, Button, Card, Space, Tag, message } from 'ant-design-vue'
import { FileAddOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import { getConfig, getSettings, saveConfig, saveSettings } from '../api/mosdns'
import { restart } from '../api/process'

const [messageApi, contextHolder] = message.useMessage()
const content = ref('')
const path = ref('')
const configured = ref(false)
const loading = ref(false)
const saving = ref(false)

async function load() {
  loading.value = true
  try {
    const cfg = await getConfig()
    content.value = cfg.content
    path.value = cfg.path
    configured.value = cfg.configured
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function save(restart: boolean) {
  saving.value = true
  try {
    await saveConfig(content.value)
    configured.value = true
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

async function generateDefault() {
  saving.value = true
  try {
    const s = await getSettings()
    await saveSettings(s)
    await load()
    messageApi.success('已生成默认配置')
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
  <Card title="配置文件（config.yaml）" size="small">
    <template #extra>
      <Tag v-if="path" style="font-family: monospace">{{ path }}</Tag>
    </template>

    <Alert
      v-if="!configured && !loading"
      type="warning"
      show-icon
      message="尚未生成配置"
      description="可点「生成默认配置」按基础设置创建一份，或在下方直接粘贴自定义 YAML。"
      style="margin-bottom: 16px"
    />

    <CodeEditor v-model="content" language="yaml" height="520px" />

    <Space style="margin-top: 12px">
      <Button type="primary" :loading="saving" @click="save(true)">
        <template #icon><SaveOutlined /></template>
        保存并重启
      </Button>
      <Button :loading="saving" @click="save(false)">
        <template #icon><SaveOutlined /></template>
        仅保存
      </Button>
      <Button :loading="loading" @click="load">
        <template #icon><ReloadOutlined /></template>
        重新加载
      </Button>
      <Button :loading="saving" @click="generateDefault">
        <template #icon><FileAddOutlined /></template>
        生成默认配置
      </Button>
    </Space>
  </Card>
</template>
