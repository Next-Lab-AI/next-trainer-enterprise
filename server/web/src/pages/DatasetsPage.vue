<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import type { UploadInstance } from "element-plus"
import { computed, onMounted, reactive, ref, watch } from "vue"
import { useI18n } from "vue-i18n"

import { datasetsApi } from "../api/resources"
import type { Dataset } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatBytes, formatTime, shortId } from "../utils/format"
import { genDatasetPassword, packEncryptedZip } from "../utils/zipcrypt"

const { t } = useI18n()
const auth = useAuthStore()
const datasets = ref<Dataset[]>([])
const loading = ref(false)
const uploadVisible = ref(false)
const uploading = ref(false)
const packPercent = ref(0)
const files = ref<File[]>([])
const mode = ref<"pack" | "zip">("pack")
const uploadRef = ref<UploadInstance>()
const form = reactive({ name: "", version: "", password: "" })

const isAdmin = computed(() => auth.user?.role === "admin")

async function refresh() {
  loading.value = true
  try {
    datasets.value = await datasetsApi.list("dataset")
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function onFileChange(_f: { raw?: File }, fileList: { raw?: File }[]) {
  files.value = fileList.map((f) => f.raw).filter((f): f is File => !!f)
  if (!form.name && files.value.length === 1) {
    form.name = files.value[0].name.replace(/\.[^.]+$/, "")
  }
}

function resetUpload() {
  files.value = []
  form.name = ""
  form.version = ""
  form.password = ""
  packPercent.value = 0
  uploadRef.value?.clearFiles()
}

async function onUpload() {
  if (files.value.length === 0) return
  uploading.value = true
  packPercent.value = 0
  try {
    const name = form.name || "dataset"
    if (mode.value === "zip") {
      const f = files.value[0]
      await datasetsApi.uploadChunked(f, f.name, name, form.version || undefined, form.password || undefined, (p) => {
        packPercent.value = Math.floor(p)
      })
    } else {
      const password = form.password || genDatasetPassword()
      const blob = await packEncryptedZip(files.value, password, (p) => {
        packPercent.value = Math.floor(p / 2)
      })
      await datasetsApi.uploadChunked(blob, `${name}.zip`, name, form.version || undefined, password, (p) => {
        packPercent.value = 50 + Math.floor(p / 2)
      })
    }
    uploadVisible.value = false
    resetUpload()
    await refresh()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    uploading.value = false
  }
}

function openUpload() {
  resetUpload()
  if (mode.value === "pack") {
    form.password = genDatasetPassword()
  }
  uploadVisible.value = true
}

watch(mode, () => {
  resetUpload()
  if (mode.value === "pack" && !form.password) {
    form.password = genDatasetPassword()
  }
})

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
      <el-button v-if="isAdmin" type="primary" @click="openUpload">{{ t("datasets.upload") }}</el-button>
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
      <el-table-column :label="t('datasets.password')" min-width="160">
        <template #default="{ row }">
          <code v-if="row.enc_key">{{ row.enc_key }}</code>
          <span v-else class="ver">-</span>
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

    <el-dialog v-model="uploadVisible" :title="t('datasets.upload')" width="480px" @closed="resetUpload">
      <el-form label-width="100px">
        <el-form-item :label="t('datasets.mode')">
          <el-radio-group v-model="mode">
            <el-radio-button value="pack">{{ t("datasets.modePack") }}</el-radio-button>
            <el-radio-button value="zip">{{ t("datasets.modeZip") }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('app.name')">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('datasets.version')">
          <el-input v-model="form.version" />
        </el-form-item>
        <el-form-item :label="t('datasets.password')">
          <el-input v-model="form.password" :placeholder="mode === 'zip' ? t('datasets.zipPasswordHint') : t('datasets.passwordHint')" />
        </el-form-item>
        <el-form-item :label="t('datasets.files')">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :multiple="mode === 'pack'"
            :limit="mode === 'zip' ? 1 : undefined"
            :accept="mode === 'zip' ? '.zip' : undefined"
            :on-change="onFileChange"
          >
            <el-button>{{ mode === "zip" ? t("datasets.selectZip") : t("datasets.selectFiles") }}</el-button>
          </el-upload>
        </el-form-item>
      </el-form>
      <el-progress v-if="uploading" :percentage="packPercent" />
      <template #footer>
        <el-button @click="uploadVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" :loading="uploading" :disabled="files.length === 0" @click="onUpload">
          {{ t("app.confirm") }}
        </el-button>
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
