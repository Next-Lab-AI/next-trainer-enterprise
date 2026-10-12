import {
  ElButton,
  ElCard,
  ElConfigProvider,
  ElContainer,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElForm,
  ElFormItem,
  ElIcon,
  ElInput,
  ElInputNumber,
  ElMain,
  ElAside,
  ElMenu,
  ElMenuItem,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
  ElUpload,
  vLoading,
} from "element-plus"
import "element-plus/theme-chalk/base.css"
import "element-plus/theme-chalk/el-button.css"
import "element-plus/theme-chalk/el-card.css"
import "element-plus/theme-chalk/el-config-provider.css"
import "element-plus/theme-chalk/el-container.css"
import "element-plus/theme-chalk/el-aside.css"
import "element-plus/theme-chalk/el-main.css"
import "element-plus/theme-chalk/el-descriptions.css"
import "element-plus/theme-chalk/el-dialog.css"
import "element-plus/theme-chalk/el-form.css"
import "element-plus/theme-chalk/el-form-item.css"
import "element-plus/theme-chalk/el-icon.css"
import "element-plus/theme-chalk/el-input.css"
import "element-plus/theme-chalk/el-input-number.css"
import "element-plus/theme-chalk/el-menu.css"
import "element-plus/theme-chalk/el-menu-item.css"
import "element-plus/theme-chalk/el-message.css"
import "element-plus/theme-chalk/el-message-box.css"
import "element-plus/theme-chalk/el-option.css"
import "element-plus/theme-chalk/el-select.css"
import "element-plus/theme-chalk/el-switch.css"
import "element-plus/theme-chalk/el-table.css"
import "element-plus/theme-chalk/el-table-column.css"
import "element-plus/theme-chalk/el-tag.css"
import "element-plus/theme-chalk/el-upload.css"
import "element-plus/theme-chalk/el-loading.css"
import { createPinia } from "pinia"
import { createApp } from "vue"

import App from "./App.vue"
import { setUnauthorizedHandler } from "./api/client"
import { i18n } from "./i18n"
import router from "./router"

const components = [
  ElButton,
  ElCard,
  ElConfigProvider,
  ElContainer,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElForm,
  ElFormItem,
  ElIcon,
  ElInput,
  ElInputNumber,
  ElMain,
  ElAside,
  ElMenu,
  ElMenuItem,
  ElOption,
  ElSelect,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
  ElUpload,
]

const app = createApp(App)
for (const component of components) {
  if (component.name) {
    app.component(component.name, component)
  }
}

setUnauthorizedHandler(() => {
  if (router.currentRoute.value.name !== "login") {
    router.push({ name: "login", query: { redirect: router.currentRoute.value.fullPath } })
  }
})

app.directive("loading", vLoading)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.config.globalProperties.$message = ElMessage
app.config.globalProperties.$msgbox = ElMessageBox
app.mount("#app")

export { ElMessage, ElMessageBox }
