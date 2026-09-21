<script setup lang="ts">
// 模型：上游当前开放的模型与上下文长度。数据由网关动态拉取（缓存 1 小时），
// 拉不到时回落内置静态表，页面会标注数据来源。
import { computed, ref } from 'vue'
import { api, type ModelInfo } from '../api'
import { guard } from '../store'
import PageHead from '../components/PageHead.vue'
import { n } from '../format'

const models = ref<ModelInfo[]>([])
const loadedAt = ref<number>(0)
const busy = ref(false)

// 模型表变化很慢，不必轮询：进页面拉一次，需要时手动刷新。
async function load() {
  busy.value = true
  const res = await guard(() => api.models())
  busy.value = false
  if (res) {
    models.value = res.models
    loadedAt.value = Date.now()
  }
}
void load()

// 上游 credits 字段两个域形态不一致：CN 是 "x0.21"，global 是 "x0.34 credits"。
// 统一取 x 后的数字；缺失（上游未给）显示 —，形态不认识时原样展示。
function creditRate(raw?: string): { text: string; tone: string } {
  const v = (raw ?? '').trim()
  if (!v) return { text: '—', tone: '' }
  const hit = /x\s*([0-9]+(?:\.[0-9]+)?)/i.exec(v)
  if (!hit) return { text: v, tone: '' }
  const val = Number(hit[1])
  if (val === 0) return { text: 'x0.00 免费', tone: 'ok' }
  if (val >= 1) return { text: `x${val.toFixed(2)}`, tone: 'warn' }
  return { text: `x${val.toFixed(2)}`, tone: '' }
}

// id 形如 "cn:deepseek-v4-flash" / "global:gpt-5.4"：拆出域做标签，模型名单独展示。
const sorted = computed(() =>
  [...models.value]
    .map((m) => {
      const [maybeRealm, ...rest] = m.id.split(':')
      const hasRealm = rest.length > 0
      return { ...m, realm: hasRealm ? maybeRealm : '', name: hasRealm ? rest.join(':') : m.id }
    })
    .sort((a, b) => a.id.localeCompare(b.id)),
)
const maxContext = computed(() => models.value.reduce((m, x) => Math.max(m, x.context_length || 0), 0))
</script>

<template>
  <div class="view">
    <PageHead
      title="模型"
      lede="网关透传的上游模型清单。客户端拿 /v1/models 得到的就是这份数据，用哪个模型由请求里的 model 字段决定。"
    >
      <template #actions>
        <span class="muted small">{{ models.length }} 个模型</span>
        <button type="button" :disabled="busy" @click="load">{{ busy ? '拉取中…' : '刷新' }}</button>
      </template>
    </PageHead>

    <section class="panel">
      <div class="panel-head">
        <h2>可用模型</h2>
        <span class="note">倍率 = 上游积分扣费倍数（x0.00 为限时免费，越高越费积分）</span>
        <span v-if="loadedAt" class="note">更新于 {{ new Date(loadedAt).toLocaleTimeString('zh-CN') }}</span>
      </div>

      <div v-if="!sorted.length" class="empty">
        <strong>没拿到模型列表</strong>
        网关会向池内任一健康账号请求模型表；一个可用账号都没有时会回落到内置静态表。等有账号可用再刷新。
      </div>

      <table v-else class="data">
        <thead>
          <tr>
            <th>模型</th>
            <th>域</th>
            <th class="n">积分倍率</th>
            <th>推理档位</th>
            <th class="n">上下文长度</th>
            <th class="n">单次最大输出</th>
            <th>相对上下文</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in sorted" :key="m.id">
            <td class="mono">{{ m.name }}</td>
            <td>
              <span v-if="m.realm" class="tag">{{ m.realm === 'global' ? '国际版' : '国内版' }}</span>
              <span v-else class="muted small">—</span>
            </td>
            <td class="n">
              <span class="tag" :class="creditRate(m.credits).tone">{{ creditRate(m.credits).text }}</span>
            </td>
            <td class="small muted">
              <template v-if="m.reasoning_supported_efforts?.length">
                {{ m.reasoning_supported_efforts.join(' / ') }}
                <template v-if="m.reasoning_default_effort">（默认 {{ m.reasoning_default_effort }}）</template>
              </template>
              <template v-else>—</template>
            </td>
            <td class="n">{{ n(m.context_length) }}</td>
            <td class="n">{{ m.max_output_tokens ? n(m.max_output_tokens) : '—' }}</td>
            <td style="width: 30%">
              <div class="credit">
                <div class="track" style="height: 5px">
                  <span
                    class="seg on"
                    :style="`flex: 0 0 ${maxContext ? Math.round(((m.context_length || 0) / maxContext) * 100) : 0}%; max-width: 100%`"
                  ></span>
                  <span class="seg" style="flex: 1"></span>
                </div>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>怎么用</h2>
      </div>
      <div class="panel-body">
        <p class="muted" style="margin-top: 0">
          任何 OpenAI 兼容客户端把 baseURL 指向网关即可，模型名照上面填：
        </p>
        <pre style="font-family: var(--mono); font-size: 12px; background: var(--surface-2); border: 1px solid var(--line); border-radius: 3px; padding: 12px; overflow-x: auto; margin: 0">baseURL: http://&lt;网关地址&gt;:7863/v1
apiKey:  &lt;调用密钥页里生成的 key&gt;
model:   {{ sorted[0]?.id || 'deepseek-v4-flash' }}</pre>
      </div>
    </section>
  </div>
</template>