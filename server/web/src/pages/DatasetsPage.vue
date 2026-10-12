<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { computed, onMounted, reactive, ref } from "vue"
import { useI18n } from "vue-i18n"

import { datasetsApi } from "../api/resources"
import type { Dataset } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatBytes, formatTime, shortId } from "../utils/format"

const { t } = useI18n()
const auth = useAuthStore()
const datasets = ref<Dataset[]>([])
const loading = ref(false)
const uploadVisible = ref(false)
const uploading = ref(false)
const file = ref<File | null>(null)
const form = reactive({ name: "", version: "", kind: "dataset" })

const isAdmin = computed(() => auth.user?.role === "admin")

async function refresh() {
  loading.value = true
  try {
    datasets.value = await datasetsApi.list()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function onFileChange(f: { raw?: File }) {
  file.value = f.raw ?? null
  if (!form.name && f.raw) {
    form.name = f.raw.name
  }
}

async function onUpload() {
  if (!file.value) return
  uploading.value = true
  try {
    await datasetsApi.upload(file.value, form.name, form.kind, form.version || undefined)
    uploadVisible.value = false
    file.value = null
    form.name = ""
    form.version = ""
    await refresh()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    uploading.value = false
  }
}

async function onDelete(d: Dataset) {
  await ElMessageBox.confirm(t("datasets.deleteConfirm"), t("app.delete"), { type: "warning" })
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
      <h2>{{ t("datasets.title") }}</h2>
      <el-button v-if="isAdmin" type="primary" @click="uploadVisible = true">{{ t("datasets.upload") }}</el-button>
    </div>
    <el-table v-loading="loading" :data="datasets">
      <el-table-column :label="t('app.name')" min-width="200">
        <template #default="{ row }">
          {{ row.name }}<span v-if="row.version" class="ver">@{{ row.version }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.kind')" prop="kind" width="100">
        <template #default="{ row }">
          <el-tag>{{ row.kind }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.size')" width="110">
        <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
      </el-table-column>
      <el-table-column :label="t('datasets.sha256')" min-width="140">
        <template #default="{ row }">
          <code>{{ shortId(row.sha256) }}</code>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column v-if="isAdmin" :label="t('app.actions')" width="100">
        <template #default="{ row }">
          <el-button size="small" type="danger" @click="onDelete(row)">{{ t("app.delete") }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="uploadVisible" :title="t('datasets.upload')" width="480px">
      <el-form label-width="100px">
        <el-form-item :label="t('app.name')">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('datasets.version')">
          <el-input v-model="form.version" />
        </el-form-item>
        <el-form-item :label="t('datasets.kind')">
          <el-select v-model="form.kind">
            <el-option label="dataset" value="dataset" />
            <el-option label="engine" value="engine" />
          </el-select>
        </el-form-item>
        <el-form-item label="File">
          <el-upload :auto-upload="false" :limit="1" :on-change="onFileChange">
            <el-button>{{ t("datasets.upload") }}</el-button>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" :loading="uploading" :disabled="!file" @click="onUpload">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.ver {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
