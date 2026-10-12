<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { computed, onMounted, onUnmounted, ref } from "vue"
import { useI18n } from "vue-i18n"

import { jobsApi } from "../api/resources"
import type { Job } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatTime, shortId, statusTagType } from "../utils/format"

const { t } = useI18n()
const auth = useAuthStore()
const jobs = ref<Job[]>([])
const loading = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const isAdmin = computed(() => auth.user?.role === "admin")

function isActive(j: Job) {
  return j.status === "QUEUED" || j.status === "CREATED" || j.status === "WAITING" || j.status === "RUNNING"
}

function canOperate(j: Job) {
  return isAdmin.value || j.user_id === auth.user?.id
}

async function refresh(silent = false) {
  if (!silent) loading.value = true
  try {
    jobs.value = await jobsApi.list()
  } catch (err) {
    if (!silent) ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function onStop(j: Job, preempt: boolean) {
  await ElMessageBox.confirm(t(preempt ? "jobs.preemptConfirm" : "jobs.stopConfirm"), t(preempt ? "jobs.preempt" : "jobs.stop"), {
    type: "warning",
  })
  await jobsApi.stop(j.job_id, preempt)
  await refresh(true)
}

async function onKill(j: Job) {
  await ElMessageBox.confirm(t("jobs.killConfirm"), t("jobs.kill"), { type: "error" })
  await jobsApi.kill(j.job_id)
  await refresh(true)
}

onMounted(async () => {
  await auth.fetchMe()
  await refresh()
  timer = setInterval(() => refresh(true), 5000)
})
onUnmounted(() => timer && clearInterval(timer))
</script>

<template>
  <div>
    <div class="header">
      <h2>{{ t("jobs.title") }}</h2>
      <router-link to="/jobs/submit">
        <el-button type="primary">{{ t("jobs.submit") }}</el-button>
      </router-link>
    </div>
    <el-table v-loading="loading" :data="jobs">
      <el-table-column label="ID" width="120">
        <template #default="{ row }">
          <router-link :to="`/jobs/${row.job_id}`">{{ shortId(row.job_id) }}</router-link>
        </template>
      </el-table-column>
      <el-table-column :label="t('jobs.engine')" width="130">
        <template #default="{ row }">{{ row.engine }}<span v-if="row.engine_version">@{{ row.engine_version }}</span></template>
      </el-table-column>
      <el-table-column :label="t('app.status')" width="120">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('jobs.gpus')" width="90">
        <template #default="{ row }">{{ JSON.parse(row.gpus || "[]").length || "-" }}</template>
      </el-table-column>
      <el-table-column :label="t('jobs.exitCode')" width="90">
        <template #default="{ row }">{{ row.exit_code ?? "-" }}</template>
      </el-table-column>
      <el-table-column :label="t('jobs.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('app.actions')" width="240">
        <template #default="{ row }">
          <template v-if="isActive(row) && canOperate(row)">
            <el-button size="small" @click="onStop(row, false)">{{ t("jobs.stop") }}</el-button>
            <el-button v-if="isAdmin" size="small" type="warning" @click="onStop(row, true)">{{ t("jobs.preempt") }}</el-button>
            <el-button size="small" type="danger" @click="onKill(row)">{{ t("jobs.kill") }}</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
