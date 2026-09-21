import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { state, setTheme } from './store'
import { api, ApiError, tokenStore } from './api'
import './style.css'

// 字体自托管（打进二进制，不依赖外部 CDN）：拉丁字母与数字用 IBM Plex，
// 中文由系统 CJK 字体接管（见 style.css 的 --sans 栈）。
// @font-face 写在 style.css 里，只取 latin 子集，避免把西里尔/希腊字母一起打进包。
import './fonts.css'

setTheme(state.theme)

// 启动先探一次 /session：既确认 token 是否还有效，也拿到 service/version 与
// "是否需要鉴权"（服务端 token 为空时后台直接可用，不该出现登录页）。
async function bootstrap() {
  try {
    const info = await api.session()
    state.service = info.service
    state.version = info.version
    state.authRequired = info.auth_required
    state.authed = true
  } catch (e) {
    // 401 = token 过期/未登录，交给路由守卫送去登录页；网络不通也照样挂载，
    // 登录页的提交会给出"连不上网关"的明确报错。
    if (e instanceof ApiError && e.status === 401) {
      tokenStore.clear()
      state.authed = false
    }
  }
  createApp(App).use(router).mount('#app')
}

void bootstrap()