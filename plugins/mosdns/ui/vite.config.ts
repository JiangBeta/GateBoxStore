import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// base: './' → 制品安装到 $DATA_DIR/tools/mosdns/ui 后由内核以 /plugins/mosdns/ 托管，
// 相对路径可保证 assets 正确解析。
export default defineConfig({
  base: './',
  plugins: [vue()],
  build: { outDir: 'dist', emptyOutDir: true },
})
