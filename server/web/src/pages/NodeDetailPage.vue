<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { computed, onMounted, reactive, ref } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute } from "vue-router"

import { nodesApi, type ProvisionRequest } from "../api/resources"
import type { BaseEnv, EngineInfo, GpuInfo, Node } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatBytes, formatTime } from "../utils/format"

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()

const node = ref<Node | null>(null)
const isAdmin = computed(() => auth.user?.role === "admin")
const provisionVisible = ref(false)
const provisionForm = reactive({
  engine: "",
  version: "",
  envMode: "venv",
  basePython: "",
})

const id = computed(() => String(route.params.id))

const gpus = computed<GpuInfo[]>(() => node.value?.gpus ?? [])

const baseEnvs = computed<BaseEnv[]>(() => node.value?.base_envs ?? [])

const stagedEngines = computed<EngineInfo[]>(() => node.value?.engines ?? [])
const stagedVersions = computed(() =>
  stagedEngines.value.filter((e) => e.name === provisionForm.engine).map((e) => e.version),
)

const labels = computed<Record<string, string>>(() => node.value?.labels ?? {})

async function refresh() {
  try {
    node.value = await nodesApi.get(id.value)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

async function onRevoke() {
  await ElMessageBox.confirm(t("nodes.revokeConfirm"), t("nodes.revoke"), { type: "warning" })
  await nodesApi.revoke(id.value)
  ElMessage.success(t("app.confirm"))
  await refresh()
}

async function onEnvGc() {
  await nodesApi.envGc(id.value)
  ElMessage.success(t("app.confirm"))
}

async function onProvision() {
  const req: ProvisionRequest = {
    engine: provisionForm.engine,
    version: provisionForm.version,
  }
  if (provisionForm.envMode === "external") {
    req.env_mode = "external"
    req.base_python = provisionForm.basePython
  }
  await nodesApi.provision(id.value, req)
  provisionVisible.value = false
  ElMessage.success(t("app.confirm"))
}

onMounted(async () => {
  await auth.fetchMe()
  await refresh()
})
</script>

<template>
  <div v-if="node">
    <div class="header">
      <h2>{{ node.hostname || node.node_id }}</h2>
      <div v-if="isAdmin" class="actions">
        <el-button @click="onEnvGc">{{ t("nodes.envGc") }}</el-button>
        <el-button type="primary" @click="provisionVisible = true">{{ t("nodes.provision") }}</el-button>
        <el-button v-if="node.status !== 'revoked'" type="danger" @click="onRevoke">{{ t("nodes.revoke") }}</el-button>
      </div>
    </div>

    <el-descriptions :column="2" border>
      <el-descriptions-item label="node_id">{{ node.node_id }}</el-descriptions-item>
      <el-descriptions-item :label="t('app.status')">
        <el-tag v-if="node.status === 'revoked'" type="danger">{{ t("nodes.revoked") }}</el-tag>
        <el-tag v-else-if="node.online" type="success">{{ t("nodes.online") }}</el-tag>
        <el-tag v-else type="info">{{ t("nodes.offline") }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item :label="t('nodes.agentVersion')">{{ node.agent_version }}</el-descriptions-item>
      <el-descriptions-item :label="t('nodes.lastSeen')">{{ formatTime(node.last_seen) }}</el-descriptions-item>
      <el-descriptions-item :label="t('nodes.description')">{{ node.description || "-" }}</el-descriptions-item>
      <el-descriptions-item :label="t('nodes.pools')">{{ node.pools.length }}</el-descriptions-item>
    </el-descriptions>

    <h3>{{ t("nodes.gpus") }}</h3>
    <el-table :data="gpus">
      <el-table-column prop="index" label="#" width="60" />
      <el-table-column prop="model" label="Model" />
      <el-table-column prop="vendor" label="Vendor" width="120" />
      <el-table-column label="Memory" width="140">
        <template #default="{ row }">{{ formatBytes(row.mem_total_mb * 1024 * 1024) }}</template>
      </el-table-column>
      <el-table-column prop="uuid" label="UUID" show-overflow-tooltip />
    </el-table>

    <h3>{{ t("nodes.baseEnvs") }}</h3>
    <el-table :data="baseEnvs">
      <el-table-column prop="python" label="Python" />
      <el-table-column prop="torch" label="Torch" width="140" />
      <el-table-column prop="cuda" label="CUDA" width="140" />
    </el-table>

    <h3>{{ t("nodes.labels") }}</h3>
    <el-table :data="Object.entries(labels).map(([k, v]) => ({ k, v }))">
      <el-table-column prop="k" label="Key" width="200" />
      <el-table-column prop="v" label="Value" />
    </el-table>

    <el-dialog v-model="provisionVisible" :title="t('nodes.provision')" width="560px">
      <el-form label-width="140px">
        <el-form-item :label="t('jobs.engine')">
          <el-select v-model="provisionForm.engine" @change="provisionForm.version = ''">
            <el-option v-for="e in stagedEngines" :key="e.name" :label="e.name" :value="e.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('jobs.engineVersion')">
          <el-select v-model="provisionForm.version">
            <el-option v-for="v in stagedVersions" :key="v" :label="v" :value="v" />
          </el-select>
        </el-form-item>
        <el-form-item label="env_mode">
          <el-select v-model="provisionForm.envMode">
            <el-option label="venv" value="venv" />
            <el-option label="external" value="external" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="provisionForm.envMode === 'external'" label="base_python">
          <el-select v-model="provisionForm.basePython">
            <el-option v-for="e in baseEnvs" :key="e.python" :label="e.python" :value="e.python" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="provisionVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onProvision">{{ t("app.confirm") }}</el-button>
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
.actions {
  display: flex;
  gap: 8px;
}
</style>
