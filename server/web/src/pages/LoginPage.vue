<script setup lang="ts">
import { ElMessage } from "element-plus"
import { reactive, ref } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute, useRouter } from "vue-router"

import { useAuthStore } from "../stores/auth"

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const form = reactive({ username: "", password: "" })
const loading = ref(false)

async function onSubmit() {
  if (!form.username || !form.password) return
  loading.value = true
  try {
    await auth.login(form.username, form.password)
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/"
    router.push(redirect)
  } catch {
    ElMessage.error(t("login.failed"))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <h2 class="title">{{ t("app.brand") }}</h2>
      <el-form @submit.prevent="onSubmit">
        <el-form-item :label="t('app.username')">
          <el-input v-model="form.username" autocomplete="username" />
        </el-form-item>
        <el-form-item :label="t('app.password')">
          <el-input v-model="form.password" type="password" autocomplete="current-password" @keyup.enter="onSubmit" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" style="width: 100%" @click="onSubmit">
            {{ t("app.login") }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
}
.login-card {
  width: 360px;
}
.title {
  text-align: center;
  margin-top: 0;
}
</style>
