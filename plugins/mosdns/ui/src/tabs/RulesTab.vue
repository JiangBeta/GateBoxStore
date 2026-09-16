<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Alert, Button, Card, Input, Space, Tabs, TabPane, Tag, message } from 'ant-design-vue'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons-vue'
import { getRule, listRules, saveRule, type RuleMeta } from '../api/mosdns'
import { restart } from '../api/process'

const [messageApi, contextHolder] = message.useMessage()
const rules = ref<RuleMeta[]>([])
const contents = reactive<Record<string, string>>({})
const active = ref('')
const loading = ref(false)
const saving = ref(false)

const current = computed(() => rules.value.find((r) => r.name === active.value))

async function load() {
  loading.value = true
  try {
    rules.value = await listRules()
    if (!active.value) active.value = rules.value[0]?.name || ''
    await Promise.all(
      rules.value.map(async (r) => {
        contents[r.name] = (await getRule(r.name)).content
      }),
    )
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function save(restart: boolean) {
  if (!active.value) return
  saving.value = true
  try {
    await saveRule(active.value, contents[active.value] ?? '')
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
  <Card title="规则列表" size="small">
    <Alert
      type="info"
      show-icon
      message="每行一条 mosdns 域名规则：plain / domain: / full: / keyword: / regexp:；以 # 开头为注释"
      style="margin-bottom: 16px"
    />

    <Tabs v-model:active-key="active">
      <TabPane v-for="r in rules" :key="r.name" :tab="r.label">
        <div style="margin-bottom: 8px; color: #888; font-size: 12px">{{ r.help }}</div>
        <Input.TextArea
          v-model:value="contents[r.name]"
          :rows="18"
          :disabled="loading"
          style="font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12.5px"
          placeholder="每行一条规则"
        />
      </TabPane>
    </Tabs>

    <Space style="margin-top: 12px">
      <Button type="primary" :loading="saving" :disabled="!current" @click="save(true)">
        <template #icon><SaveOutlined /></template>
        保存并重启
      </Button>
      <Button :loading="saving" :disabled="!current" @click="save(false)">
        <template #icon><SaveOutlined /></template>
        仅保存
      </Button>
      <Button :loading="loading" @click="load">
        <template #icon><ReloadOutlined /></template>
        重新加载
      </Button>
      <Tag v-if="current" style="font-family: monospace">rule/{{ current.name }}.txt</Tag>
    </Space>
  </Card>
</template>
