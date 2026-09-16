import { createApp } from 'vue'
import 'ant-design-vue/dist/reset.css'
import App from './App.vue'
import { bootstrap, setupBridge } from './bridge'

bootstrap().finally(() => {
  setupBridge()
  createApp(App).mount('#app')
})
