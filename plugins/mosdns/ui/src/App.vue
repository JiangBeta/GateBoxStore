<script setup lang="ts">
import { Tabs, TabPane } from 'ant-design-vue'
import { activeTab } from './tabs/state'
import StatusTab from './tabs/StatusTab.vue'
import BasicTab from './tabs/BasicTab.vue'
import HostsTab from './tabs/HostsTab.vue'
import RulesTab from './tabs/RulesTab.vue'
import GeodataTab from './tabs/GeodataTab.vue'
import ConfigTab from './tabs/ConfigTab.vue'
import LogsTab from './tabs/LogsTab.vue'
</script>

<template>
  <div style="padding: 16px">
    <Tabs v-model:activeKey="activeTab">
      <TabPane key="status" tab="状态"><StatusTab /></TabPane>
      <TabPane key="basic" tab="基础设置"><BasicTab /></TabPane>
      <TabPane key="hosts" tab="内网解析"><HostsTab /></TabPane>
      <TabPane key="rules" tab="规则"><RulesTab /></TabPane>
      <TabPane key="geodata" tab="数据库"><GeodataTab /></TabPane>
      <TabPane key="config" tab="配置文件"><ConfigTab /></TabPane>
      <TabPane key="logs" tab="日志"><LogsTab /></TabPane>
    </Tabs>
  </div>
</template>
