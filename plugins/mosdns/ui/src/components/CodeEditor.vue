<script setup lang="ts">
/**
 * 可复用 CodeMirror 编辑器组件。
 * 用法：
 *   <CodeEditor v-model="code" language="json" height="320px" />
 * Props: modelValue, language('json'|'yaml'), editable, height, readOnly
 * Emits: update:modelValue
 * Expose: undo, redo, search, focus, setDoc, getDoc, requestMeasure
 */
import { ref, watch, computed, onBeforeUnmount, nextTick } from 'vue'
import { Button, Select, Tooltip, message } from 'ant-design-vue'
import {
  UndoOutlined, RedoOutlined, SearchOutlined, CopyOutlined, BulbOutlined, BulbFilled,
} from '@ant-design/icons-vue'
import '@fontsource/maple-mono/latin-400.css'
import '@fontsource/maple-mono/latin-700.css'

const props = withDefaults(defineProps<{
  modelValue?: string
  language?: 'json' | 'yaml'
  editable?: boolean
  height?: string
  readOnly?: boolean
}>(), {
  modelValue: '',
  language: 'json',
  editable: true,
  height: '320px',
  readOnly: false,
})

const [messageApi, contextHolder] = message.useMessage()

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

// --- State ---
const editorEl = ref<HTMLElement | null>(null)
let editorView: any = null
let cmCache: any = null

const editorTheme = ref<'dark' | 'light'>('dark')
const editorFontSize = ref(13)
const editorFontFamily = ref('Maple Mono')
const fontSizeOptions = [12, 13, 14, 16, 18].map((n) => ({ label: `${n}px`, value: n }))
const fontFamilyOptions = [
  { label: 'Maple Mono', value: 'Maple Mono' },
  { label: 'Fira Code', value: 'Fira Code' },
  { label: 'JetBrains Mono', value: 'JetBrains Mono' },
  { label: 'Cascadia Code', value: 'Cascadia Code' },
  { label: 'Source Code Pro', value: 'Source Code Pro' },
  { label: 'IBM Plex Mono', value: 'IBM Plex Mono' },
  { label: '系统等宽', value: 'monospace' },
  { label: 'ui-monospace', value: 'ui-monospace' },
]

let themeCompartment: any = null
let fontSizeCompartment: any = null
let fontFamilyCompartment: any = null
let editableCompartment: any = null

// --- Dynamic imports ---
async function loadCm() {
  if (cmCache) return cmCache
  const [cm, langJson, langYaml, state, search, oneDark, view, commands] = await Promise.all([
    import('codemirror'),
    import('@codemirror/lang-json'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/state'),
    import('@codemirror/search'),
    import('@codemirror/theme-one-dark'),
    import('@codemirror/view'),
    import('@codemirror/commands'),
  ])
  cmCache = { cm, langJson, langYaml, state, search, oneDark, view, commands }
  return cmCache
}

// --- Theme ---
const lightTheme = (cm: any) => cm.EditorView.theme({
  '&': { backgroundColor: '#ffffff', color: '#333333' },
  '.cm-gutters': { backgroundColor: '#f5f5f5', color: '#999999', borderRight: '1px solid #e0e0e0' },
  '.cm-activeLineGutter': { backgroundColor: '#e8e8e8' },
  '.cm-activeLine': { backgroundColor: '#f0f0f0' },
  '.cm-cursor': { borderLeftColor: '#333333' },
  '.cm-selectionBackground': { backgroundColor: '#d4e6f1 !important' },
  '.cm-search button, .cm-search .cm-button': { color: '#333' },
})

function themedExtensions(cm: any, oneDark: any) {
  const btn = cm.EditorView.theme({
    '.cm-search button, .cm-search .cm-button': { color: editorTheme.value === 'dark' ? '#abb2bf' : '#333' },
  })
  return editorTheme.value === 'dark' ? [oneDark.oneDark, btn] : [lightTheme(cm), btn]
}

function toggleTheme() {
  editorTheme.value = editorTheme.value === 'dark' ? 'light' : 'dark'
  reconfigureTheme()
}

function reconfigureTheme() {
  if (!editorView || !cmCache) return
  editorView.dispatch({ effects: themeCompartment.reconfigure(themedExtensions(cmCache.cm, cmCache.oneDark)) })
}

function setFontSize(px: number) {
  editorFontSize.value = px
  if (editorView && cmCache) {
    editorView.dispatch({ effects: fontSizeCompartment.reconfigure(cmCache.cm.EditorView.theme({ '&': { fontSize: `${px}px` } })) })
  }
}

function setFontFamily(family: string) {
  editorFontFamily.value = family
  if (editorView && cmCache) {
    editorView.dispatch({ effects: fontFamilyCompartment.reconfigure(cmCache.cm.EditorView.theme({ '.cm-content': { fontFamily: `${family}, ui-monospace, SFMono-Regular, Menlo, monospace` } })) })
  }
}

// --- Toolbar actions ---
function undoEdit() { if (editorView && cmCache) cmCache.commands.undo(editorView) }
function redoEdit() { if (editorView && cmCache) cmCache.commands.redo(editorView) }
function openSearch() { if (editorView && cmCache) cmCache.search.openSearchPanel(editorView) }

async function copyTextToClipboard(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;border:none;outline:none;padding:0'
  document.body.appendChild(ta)
  ta.focus({ preventScroll: true })
  ta.select()
  ta.setSelectionRange(0, text.length)
  let ok = false
  try { ok = document.execCommand('copy') } catch { ok = false }
  document.body.removeChild(ta)
  if (!ok) throw new Error('copy failed')
}

function copyCode() {
  const doc = editorView?.state.doc.toString() || ''
  if (!doc) { messageApi.warning('编辑器内容为空'); return }
  copyTextToClipboard(doc)
    .then(() => messageApi.success('已复制'))
    .catch(() => messageApi.error('复制失败，请手动 Ctrl+C'))
}

// --- Whitespace markers ---
function whitespaceMarkers(view: any, state: any) {
  const { ViewPlugin, Decoration, WidgetType } = view
  const { RangeSetBuilder } = state
  const mk = (txt: string) => class extends WidgetType {
    eq() { return true }
    toDOM() {
      const s = document.createElement('span')
      s.textContent = txt
      s.style.color = '#808080'
      s.style.opacity = '0.55'
      return s
    }
  }
  const SpaceWidget = mk('·')
  const TabWidget = mk('→')
  return ViewPlugin.fromClass(class {
    decorations: any
    constructor(view: any) { this.decorations = this.build(view) }
    update(u: any) { if (u.docChanged || u.viewportChanged) this.decorations = this.build(u.view) }
    build(view: any) {
      const b = new RangeSetBuilder()
      for (const { from, to } of view.visibleRanges) {
        const text = view.state.doc.sliceString(from, to)
        for (let i = 0; i < text.length; i++) {
          const c = text.charCodeAt(i)
          if (c === 32) b.add(from + i, from + i + 1, Decoration.replace({ widget: new SpaceWidget() }))
          else if (c === 9) b.add(from + i, from + i + 1, Decoration.replace({ widget: new TabWidget() }))
        }
      }
      return b.finish()
    }
  }, { decorations: (v: any) => v.decorations })
}

// --- Language loader ---
async function loadLang(cm: any) {
  if (props.language === 'yaml') {
    const mod = await cmCache.langYaml
    return mod.yaml()
  }
  const mod = await cmCache.langJson
  return mod.json()
}

// --- Init / Destroy ---
async function initEditor() {
  if (!editorEl.value || editorView) return
  const { cm, state, search, oneDark, view } = await loadCm()
  themeCompartment = new state.Compartment()
  fontSizeCompartment = new state.Compartment()
  fontFamilyCompartment = new state.Compartment()
  editableCompartment = new state.Compartment()
  const lang = await loadLang(cm)
  const isFlex = props.height === '100%'
  editorView = new cm.EditorView({
    doc: props.modelValue,
    extensions: [
      cm.basicSetup,
      lang,
      state.EditorState.tabSize.of(2),
      cm.EditorView.lineWrapping,
      search.search({ top: true }),
      state.EditorState.phrases.of({
        'Find': '查找', 'Replace': '替换', 'replace': '替换',
        'replace all': '全部替换', 'next': '下一个', 'previous': '上一个',
        'all': '全部', 'match case': '区分大小写', 'by word': '全字匹配',
        'regexp': '正则', 'close': '关闭',
      }),
      themeCompartment.of(themedExtensions(cm, oneDark)),
      fontSizeCompartment.of(cm.EditorView.theme({ '&': { fontSize: `${editorFontSize.value}px` } })),
      fontFamilyCompartment.of(cm.EditorView.theme({ '.cm-content': { fontFamily: `${editorFontFamily.value}, ui-monospace, SFMono-Regular, Menlo, monospace` } })),
      editableCompartment.of(cm.EditorView.editable.of(props.editable)),
      cm.EditorView.theme({
        '&': { height: '100%', minHeight: '0' },
        '.cm-editor': { display: 'flex', flexDirection: 'column' },
        // 修复高度过大无滚动条/滚轮不可滚:让 .cm-scroller 成为滚动容器并撑满高度
        '.cm-scroller': { flex: '1', minHeight: '0', overflow: 'auto', overscrollBehavior: 'contain' },
        '.cm-content': { minHeight: '100%' },
      }),
      cm.EditorView.updateListener.of((u: any) => {
        if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
      }),
      whitespaceMarkers(view, state),
    ],
    parent: editorEl.value,
  })
  nextTick(() => editorView?.requestMeasure())
}

function destroyEditor() {
  if (editorView) { editorView.destroy(); editorView = null }
}

// --- Watch editable prop ---
watch(() => props.editable, (v) => {
  if (editorView && editableCompartment) {
    editorView.dispatch({ effects: editableCompartment.reconfigure(v) })
  }
})

// --- Watch modelValue 外部变化(如切换编辑对象)同步进编辑器 ---
watch(() => props.modelValue, (v) => {
  const cur = getDoc()
  if (v !== cur) setDoc(v ?? '')
})

// --- Expose ---
function setDoc(doc: string) { if (editorView) editorView.dispatch({ changes: { from: 0, to: editorView.state.doc.length, insert: doc } }) }
function getDoc() { return editorView?.state.doc.toString() || '' }
function focus() { editorView?.focus() }
function requestMeasure() { editorView?.requestMeasure() }
/** 在光标处插入文本(供变量点击等场景使用),插入后光标置于文本之后。 */
function insertAtCursor(text: string) {
  if (!editorView) return
  const sel = editorView.state.selection.main
  const after = sel.from + text.length
  editorView.dispatch({
    changes: { from: sel.from, to: sel.to, insert: text },
    selection: { anchor: after, head: after },
  })
  editorView.focus()
}

defineExpose({ undo: undoEdit, redo: redoEdit, search: openSearch, focus, setDoc, getDoc, requestMeasure, insertAtCursor, initEditor, destroyEditor })

const rootStyle = computed(() => {
  const isFlex = props.height === '100%'
  return {
    display: 'flex', flexDirection: 'column' as const,
    border: `1px solid ${editorTheme.value === 'dark' ? '#333' : '#d0d0d0'}`,
    borderRadius: '6px', overflow: 'hidden',
    ...(isFlex ? { flex: '1', minHeight: '0' } : {}),
  }
})

const editorStyle = computed(() => {
  const isFlex = props.height === '100%'
  return {
    height: isFlex ? undefined : props.height,
    flex: isFlex ? '1' : undefined,
    minHeight: isFlex ? '0' : undefined,
    overflow: 'hidden',
    background: editorTheme.value === 'dark' ? '#282c34' : '#ffffff',
  }
})
</script>

<template>
  <contextHolder />
  <div :style="rootStyle">
    <!-- 工具栏 -->
    <div :style="{ background: editorTheme === 'dark' ? '#282c34' : '#fafafa', color: editorTheme === 'dark' ? '#abb2bf' : '#333', borderBottom: '1px solid ' + (editorTheme === 'dark' ? '#181a1f' : '#e0e0e0') }"
      style="display: flex; align-items: center; gap: 6px; padding: 4px 8px; flex-shrink: 0">
      <Tooltip title="撤销">
        <Button size="small" type="text" @click="undoEdit">
          <UndoOutlined :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
        </Button>
      </Tooltip>
      <Tooltip title="重做">
        <Button size="small" type="text" @click="redoEdit">
          <RedoOutlined :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
        </Button>
      </Tooltip>
      <Tooltip title="查找">
        <Button size="small" type="text" @click="openSearch">
          <SearchOutlined :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
        </Button>
      </Tooltip>
      <Tooltip :title="editorTheme === 'dark' ? '切换到浅色' : '切换到深色'">
        <Button size="small" type="text" @click="toggleTheme">
          <BulbOutlined v-if="editorTheme === 'dark'" :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
          <BulbFilled v-else :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
        </Button>
      </Tooltip>
      <Select :value="editorFontSize" :options="fontSizeOptions" size="small" style="width: 72px" @update:value="(v: number) => setFontSize(v)" />
      <Select :value="editorFontFamily" :options="fontFamilyOptions" size="small" style="width: 120px" @update:value="(v: string) => setFontFamily(v)" />
      <div style="flex: 1"></div>
      <Tooltip title="复制">
        <Button size="small" type="text" @click="copyCode">
          <CopyOutlined :style="{ color: editorTheme === 'dark' ? '#abb2bf' : '#333' }" />
        </Button>
      </Tooltip>
    </div>
    <!-- 编辑器 -->
    <div ref="editorEl" :style="editorStyle" @click="() => { if (!editorView) initEditor() }" />
  </div>
</template>
