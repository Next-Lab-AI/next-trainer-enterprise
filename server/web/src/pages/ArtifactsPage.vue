<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"

import { datasetsApi } from "../api/resources"
import type { Dataset } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatBytes, formatTime } from "../utils/format"

const { t } = useI18n()
const auth = useAuthStore()
const artifacts = ref<Dataset[]>([])
const loading = ref(false)

const isAdmin = computed(() => auth.user?.role === "admin")

function displayName(d: Dataset): string {
  const idx = d.name.indexOf("/")
  return idx >= 0 ? d.name.slice(idx + 1) : d.name
}

async function refresh() {
  loading.value = true
  try {
    artifacts.value = await datasetsApi.list("artifact")
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function onDownload(d: Dataset) {
  try {
    await datasetsApi.download(d.dataset_id)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

async function onDelete(d: Dataset) {
  await ElMessageBox.confirm(t("artifacts.deleteConfirm"), t("app.delete"), { type: "warning" })
  await datasetsApi.remove(d.dataset_id)
  await refresh()
}

onMounted(async () => {
  await auth.fetchMe()
  await refresh()
})
</script>

<template>
  <div>
    <div class="header">
      <h2>{{ t("artifacts.title") }}</h2>
      <el-button @click="refresh">{{ t("app.refresh") }}</el-button>
    </div>
    <el-table v-loading="loading" :data="artifacts">
      <el-table-column :label="t('app.name')" min-width="240">
        <template #default="{ row }">{{ displayName(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('artifacts.job')" min-width="160">
        <template #default="{ row }">
          <router-link v-if="row.job_id" :to="`/jobs/${row.job_id}`">
            <code>{{ row.job_id.slice(0, 8) }}</code>
          </router-link>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.size')" width="110">
        <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
      </el-table-column>
      <el-table-column :label="t('datasets.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('app.actions')" width="180">
        <template #default="{ row }">
          <el-button size="small" @click="onDownload(row)">{{ t("app.download") }}</el-button>
          <el-button v-if="isAdmin" size="small" type="danger" @click="onDelete(row)">{{ t("app.delete") }}</el-button>
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
