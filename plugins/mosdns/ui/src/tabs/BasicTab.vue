<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  Alert, Button, Card, Divider, Form, FormItem, Input, InputNumber, Select, Space, Switch, Tabs, TabPane, message,
} from 'ant-design-vue'
import { DownloadOutlined, ReloadOutlined, RestOutlined, SaveOutlined } from '@ant-design/icons-vue'
import {
  DEFAULT_SETTINGS, getSettings, saveSettings, updateAdblock, type MosdnsSettings,
} from '../api/mosdns'
import { restart } from '../api/process'

const [messageApi, contextHolder] = message.useMessage()
const loading = ref(false)
const saving = ref(false)
const downloading = ref(false)
const activeKey = ref('basic')

const form = reactive<MosdnsSettings>({ ...DEFAULT_SETTINGS })

const logLevelOptions = [
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' },
]

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

async function load() {
  loading.value = true
  try {
    Object.assign(form, await getSettings())
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function persist(): Promise<boolean> {
  try {
    Object.assign(form, await saveSettings(clone(form)))
    return true
  } catch (e) {
    messageApi.error((e as Error).message)
    return false
  }
}

async function save(restart: boolean) {
  saving.value = true
  try {
    if (!(await persist())) return
    if (restart) {
      await restart()
      messageApi.success('已保存并重启 mosdns')
    } else {
      messageApi.success('已保存（重启后生效）')
    }
    window.dispatchEvent(new Event('gatebox:components-changed'))
  } finally {
    saving.value = false
  }
}

async function downloadAdSources() {
  downloading.value = true
  try {
    if (!(await persist())) return
    const { results } = await updateAdblock()
    const failed = results.filter((r) => r.error)
    if (failed.length) messageApi.warning(`${failed.length} 个来源下载失败：${failed[0].name}`)
    else messageApi.success(`已下载 ${results.length} 个广告规则来源`)
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    downloading.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Card title="基础设置" size="small">
    <Alert
      type="info"
      show-icon
      message="保存将按表单重新生成 config.yaml，会覆盖「配置文件」页的手工改动"
      style="margin-bottom: 16px"
    />

    <Tabs v-model:active-key="activeKey">
      <TabPane key="basic" tab="基础">
        <Form layout="vertical" :disabled="loading" style="max-width: 760px">
          <FormItem label="DNS 监听地址" help="格式 host:port 或 :port，例如 0.0.0.0:5335 监听所有网卡">
            <Input v-model:value="form.listen" placeholder="0.0.0.0:5335" />
          </FormItem>
          <FormItem label="日志级别">
            <Select v-model:value="form.logLevel" :options="logLevelOptions" style="width: 200px" />
          </FormItem>
          <FormItem label="本地 / 国内上游 DNS" help="回车添加；支持 8.8.8.8、tls://、https://、h3://、quic:// 等格式">
            <Select v-model:value="form.localDns" mode="tags" placeholder="如 223.5.5.5、119.29.29.29" :token-separators="[',', ' ']" />
          </FormItem>
          <FormItem label="远程 / 国外上游 DNS" help="命中国外域名或本地上游判定为非国内 IP 时使用">
            <Select v-model:value="form.remoteDns" mode="tags" placeholder="如 tls://8.8.8.8" :token-separators="[',', ' ']" />
          </FormItem>
          <FormItem label="Bootstrap DNS" help="用于解析 DoH/DoT 上游的域名">
            <Input v-model:value="form.bootstrap" style="width: 240px" />
          </FormItem>
          <FormItem label="国内解析优先 IPv4">
            <Switch v-model:checked="form.preferIpv4Cn" />
          </FormItem>
          <FormItem label="Apple 域名优化" help="Apple 双栈域名优先返回国内 CDN 地址">
            <Switch v-model:checked="form.appleOptimization" />
          </FormItem>
        </Form>
      </TabPane>

      <TabPane key="advanced" tab="高级">
        <Form layout="vertical" :disabled="loading" style="max-width: 760px">
          <Space :size="24" wrap>
            <FormItem label="并发查询数">
              <InputNumber v-model:value="form.concurrent" :min="1" :max="3" />
            </FormItem>
            <FormItem label="空闲超时（秒）">
              <InputNumber v-model:value="form.idleTimeout" :min="1" />
            </FormItem>
          </Space>
          <FormItem label="TCP/DoT 连接复用（Pipeline）">
            <Switch v-model:checked="form.enablePipeline" />
          </FormItem>
          <FormItem label="跳过上游 TLS 证书校验">
            <Switch v-model:checked="form.insecureSkipVerify" />
          </FormItem>
          <FormItem label="远程上游启用 EDNS Client Subnet（ECS）">
            <Switch v-model:checked="form.enableEcsRemote" />
          </FormItem>
          <FormItem v-if="form.enableEcsRemote" label="ECS 客户端子网 IP">
            <Input v-model:value="form.remoteEcsIp" placeholder="如 110.34.181.1" style="width: 240px" />
          </FormItem>
          <FormItem label="防止 DNS 泄漏" help="fallback 主用直接走远程 DNS">
            <Switch v-model:checked="form.dnsLeak" />
          </FormItem>
          <FormItem label="远程解析优先 IPv4">
            <Switch v-model:checked="form.preferIpv4" />
          </FormItem>

          <Divider orientation="left" style="margin: 8px 0 16px">缓存</Divider>
          <FormItem label="启用 DNS 缓存">
            <Switch v-model:checked="form.cache" />
          </FormItem>
          <template v-if="form.cache">
            <Space :size="24" wrap>
              <FormItem label="缓存容量（条）">
                <InputNumber v-model:value="form.cacheSize" :min="1" :step="1024" />
              </FormItem>
              <FormItem label="Lazy Cache TTL（秒）" help="0 表示禁用懒缓存">
                <InputNumber v-model:value="form.lazyCacheTtl" :min="0" :step="3600" />
              </FormItem>
            </Space>
            <FormItem label="缓存落盘（重启后恢复）">
              <Switch v-model:checked="form.dumpFile" />
            </FormItem>
            <FormItem v-if="form.dumpFile" label="自动保存间隔（秒）">
              <InputNumber v-model:value="form.dumpInterval" :min="1" />
            </FormItem>
          </template>

          <Divider orientation="left" style="margin: 8px 0 16px">TTL 与过滤</Divider>
          <Space :size="24" wrap>
            <FormItem label="最小 TTL（秒）" help="0 表示不修改">
              <InputNumber v-model:value="form.minimalTtl" :min="0" />
            </FormItem>
            <FormItem label="最大 TTL（秒）" help="0 表示不修改">
              <InputNumber v-model:value="form.maximumTtl" :min="0" />
            </FormItem>
          </Space>
          <FormItem label="禁用 HTTPS/SVCB 记录（RR Type 65）">
            <Switch v-model:checked="form.rejectType65" />
          </FormItem>

          <Divider orientation="left" style="margin: 8px 0 16px">流媒体</Divider>
          <FormItem label="自定义流媒体 DNS">
            <Switch v-model:checked="form.customStreamMediaDns" />
          </FormItem>
          <FormItem v-if="form.customStreamMediaDns" label="流媒体上游 DNS">
            <Select v-model:value="form.streamDns" mode="tags" placeholder="如 tls://8.8.8.8" :token-separators="[',', ' ']" />
          </FormItem>

          <Divider orientation="left" style="margin: 8px 0 16px">广告拦截</Divider>
          <FormItem label="启用广告拦截" help="规则来自「规则 → 广告拦截」及下方来源（需为 mosdns 域名规则格式）">
            <Switch v-model:checked="form.adblock" />
          </FormItem>
          <template v-if="form.adblock">
            <FormItem label="规则来源" help="支持 http(s):// 链接或 file:// 本地路径，回车添加">
              <Select v-model:value="form.adSources" mode="tags" placeholder="如 https://example.com/ads.txt" />
            </FormItem>
            <Button :loading="downloading" @click="downloadAdSources">
              <template #icon><DownloadOutlined /></template>
              下载广告规则
            </Button>
          </template>
        </Form>
      </TabPane>

      <TabPane key="cloudflare" tab="Cloudflare">
        <Form layout="vertical" :disabled="loading" style="max-width: 760px">
          <Alert
            type="info"
            show-icon
            message="匹配到 Cloudflare IP 段时，把解析结果改写为自定义 IP（实验特性）"
            style="margin-bottom: 16px"
          />
          <FormItem label="启用 Cloudflare 优化">
            <Switch v-model:checked="form.cloudflare" />
          </FormItem>
          <FormItem v-if="form.cloudflare" label="自定义 IP" help="命中 Cloudflare 段时返回该地址，可填多个">
            <Select v-model:value="form.cloudflareIp" mode="tags" placeholder="如 1.2.3.4" />
          </FormItem>
          <FormItem v-if="form.cloudflare" label="Cloudflare IP 段">
            <span style="color: #888">
              在「规则 → Cloudflare IP 段」中编辑，默认需自行填入 Cloudflare 官方 v4/v6 段。
            </span>
          </FormItem>
        </Form>
      </TabPane>
    </Tabs>

    <Space style="margin-top: 8px">
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
      <Button @click="Object.assign(form, clone(DEFAULT_SETTINGS))">
        <template #icon><RestOutlined /></template>
        恢复默认
      </Button>
    </Space>
  </Card>
</template>
