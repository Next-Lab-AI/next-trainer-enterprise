<script setup lang="ts">
import { ElMessage } from "element-plus"
import { computed, onMounted, onUnmounted, ref } from "vue"
import { useI18n } from "vue-i18n"

import { adminApi, nodesApi } from "../api/resources"
import type { Node } from "../api/types"
import { useAuthStore } from "../stores/auth"
import { formatTime } from "../utils/format"

const { t } = useI18n()
const auth = useAuthStore()
const nodes = ref<Node[]>([])
const loading = ref(false)
const regOpen = ref(false)
const isAdmin = computed(() => auth.user?.role === "admin")
let timer: ReturnType<typeof setInterval> | undefined

function gpuCount(n: Node): number {
  try {
    return (JSON.parse(n.gpus) as unknown[]).length
  } catch {
    return 0
  }
}

async function refresh(silent = false) {
  if (!silent) loading.value = true
  try {
    nodes.value = await nodesApi.list()
    if (isAdmin.value) {
      regOpen.value = (await adminApi.getRegistration()).open
    }
  } catch (err) {
    if (!silent) ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function toggleRegistration(open: boolean) {
  try {
    await adminApi.setRegistration(open)
    regOpen.value = open
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
    await refresh(true)
  }
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
      <h2>{{ t("nodes.title") }}</h2>
      <div v-if="isAdmin" class="reg-toggle">
        <span>{{ t("nodes.registrationWindow") }}</span>
        <el-switch :model-value="regOpen" @change="toggleRegistration" />
        <span class="hint">{{ regOpen ? t("nodes.registrationOpen") : t("nodes.registrationClosed") }}</span>
      </div>
    </div>
    <el-table v-loading="loading" :data="nodes">
      <el-table-column :label="t('nodes.hostname')" prop="hostname">
        <template #default="{ row }">
          <router-link :to="`/nodes/${row.node_id}`">{{ row.hostname || row.node_id.slice(0, 10) }}</router-link>
        </template>
      </el-table-column>
      <el-table-column :label="t('app.status')" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.status === 'revoked'" type="danger">{{ t("nodes.revoked") }}</el-tag>
          <el-tag v-else-if="row.online" type="success">{{ t("nodes.online") }}</el-tag>
          <el-tag v-else type="info">{{ t("nodes.offline") }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('nodes.gpus')" width="80">
        <template #default="{ row }">{{ gpuCount(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('nodes.description')" prop="description" show-overflow-tooltip />
      <el-table-column :label="t('nodes.pools')" width="140">
        <template #default="{ row }">{{ row.pools.length }}</template>
      </el-table-column>
      <el-table-column :label="t('nodes.agentVersion')" prop="agent_version" width="120" />
      <el-table-column :label="t('nodes.lastSeen')" width="170">
        <template #default="{ row }">{{ formatTime(row.last_seen) }}</template>
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
.reg-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
