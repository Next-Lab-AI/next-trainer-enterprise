<script setup lang="ts">
import { computed } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute, useRouter } from "vue-router"

import { setStoredLocale, type AppLocale } from "../i18n"
import { useAuthStore } from "../stores/auth"

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const isAdmin = computed(() => auth.user?.role === "admin")
const active = computed(() => {
  const p = route.path
  if (p.startsWith("/jobs")) return "/jobs"
  if (p.startsWith("/nodes")) return "/nodes"
  return p
})

async function onLogout() {
  await auth.logout()
  router.push({ name: "login" })
}

function toggleLocale() {
  const next: AppLocale = locale.value === "zh-CN" ? "en-US" : "zh-CN"
  locale.value = next
  setStoredLocale(next)
}
</script>

<template>
  <el-container class="shell">
    <el-aside width="200px" class="side">
      <div class="brand">{{ t("app.brand") }}</div>
      <el-menu :default-active="active" router>
        <el-menu-item index="/nodes">{{ t("nav.nodes") }}</el-menu-item>
        <el-menu-item index="/jobs">{{ t("nav.jobs") }}</el-menu-item>
        <el-menu-item index="/jobs/submit">{{ t("nav.submitJob") }}</el-menu-item>
        <el-menu-item index="/datasets">{{ t("nav.datasets") }}</el-menu-item>
        <el-menu-item index="/artifacts">{{ t("nav.artifacts") }}</el-menu-item>
        <template v-if="isAdmin">
          <el-menu-item index="/admin/users">{{ t("nav.adminUsers") }}</el-menu-item>
          <el-menu-item index="/admin/pools">{{ t("nav.adminPools") }}</el-menu-item>
        </template>
      </el-menu>
      <div class="side-footer">
        <el-button size="small" text @click="toggleLocale">{{ locale === "zh-CN" ? "EN" : "中文" }}</el-button>
        <el-button size="small" text @click="onLogout">{{ t("app.logout") }}</el-button>
      </div>
    </el-aside>
    <el-main class="main">
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.shell {
  min-height: 100vh;
}
.side {
  border-right: 1px solid var(--el-border-color-light);
  display: flex;
  flex-direction: column;
}
.brand {
  padding: 16px;
  font-weight: 600;
}
.side .el-menu {
  border-right: none;
  flex: 1;
}
.side-footer {
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
}
.main {
  padding: 20px;
}
</style>
