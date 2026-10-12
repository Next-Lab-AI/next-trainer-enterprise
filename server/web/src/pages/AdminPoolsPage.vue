<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { onMounted, reactive, ref } from "vue"
import { useI18n } from "vue-i18n"

import { adminApi, datasetsApi, nodesApi, poolsApi } from "../api/resources"
import type { Dataset, Node, Pool, User } from "../api/types"
import { formatTime } from "../utils/format"

const { t } = useI18n()
const pools = ref<Pool[]>([])
const nodes = ref<Node[]>([])
const users = ref<User[]>([])
const datasets = ref<Dataset[]>([])
const createVisible = ref(false)
const createForm = reactive({ name: "" })

const assignVisible = ref(false)
const grantUserVisible = ref(false)
const grantDatasetVisible = ref(false)
const currentPool = ref<Pool | null>(null)
const assignForm = reactive({ nodeId: "", assign: true })
const grantUserForm = reactive({ userId: "", grant: true })
const grantDatasetForm = reactive({ datasetId: "", grant: true })

async function refresh() {
  pools.value = await poolsApi.list()
  nodes.value = await nodesApi.list()
}

function nodeName(id: string): string {
  return nodes.value.find((n) => n.node_id === id)?.hostname || id.slice(0, 8)
}

async function onCreate() {
  try {
    await poolsApi.create(createForm.name)
    createVisible.value = false
    createForm.name = ""
    await refresh()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

async function onDelete(p: Pool) {
  await ElMessageBox.confirm(p.name, t("app.delete"), { type: "warning" })
  await poolsApi.remove(p.pool_id)
  await refresh()
}

async function openAssign(p: Pool) {
  currentPool.value = p
  nodes.value = await nodesApi.list()
  assignForm.nodeId = ""
  assignForm.assign = true
  assignVisible.value = true
}

async function onAssign() {
  if (!currentPool.value || !assignForm.nodeId) return
  await poolsApi.assignNode(currentPool.value.pool_id, assignForm.nodeId, assignForm.assign)
  assignVisible.value = false
  ElMessage.success(t("app.confirm"))
}

async function openGrantUser(p: Pool) {
  currentPool.value = p
  users.value = await adminApi.listUsers()
  grantUserForm.userId = ""
  grantUserForm.grant = true
  grantUserVisible.value = true
}

async function onGrantUser() {
  if (!currentPool.value || !grantUserForm.userId) return
  await poolsApi.grantUser(currentPool.value.pool_id, grantUserForm.userId, grantUserForm.grant)
  grantUserVisible.value = false
  ElMessage.success(t("app.confirm"))
}

async function openGrantDataset(p: Pool) {
  currentPool.value = p
  datasets.value = await datasetsApi.list()
  grantDatasetForm.datasetId = ""
  grantDatasetForm.grant = true
  grantDatasetVisible.value = true
}

async function onGrantDataset() {
  if (!currentPool.value || !grantDatasetForm.datasetId) return
  await poolsApi.grantDataset(currentPool.value.pool_id, grantDatasetForm.datasetId, grantDatasetForm.grant)
  grantDatasetVisible.value = false
  ElMessage.success(t("app.confirm"))
}

onMounted(refresh)
</script>

<template>
  <div>
    <div class="header">
      <h2>{{ t("admin.pools") }}</h2>
      <el-button type="primary" @click="createVisible = true">{{ t("admin.createPool") }}</el-button>
    </div>
    <el-table :data="pools">
      <el-table-column :label="t('app.name')" prop="name" />
      <el-table-column :label="t('nav.nodes')">
        <template #default="{ row }">
          <span v-if="row.nodes?.length">{{ row.nodes.map(nodeName).join(", ") }}</span>
          <span v-else class="empty">-</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.autoCreated')" width="120">
        <template #default="{ row }">
          <el-tag v-if="row.auto_created" type="warning">auto</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('app.actions')" width="380">
        <template #default="{ row }">
          <el-button size="small" @click="openAssign(row)">{{ t("admin.assignNodes") }}</el-button>
          <el-button size="small" @click="openGrantUser(row)">{{ t("admin.grantUsers") }}</el-button>
          <el-button size="small" @click="openGrantDataset(row)">{{ t("admin.grantDatasets") }}</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">{{ t("app.delete") }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="createVisible" :title="t('admin.createPool')" width="420px">
      <el-form label-width="100px">
        <el-form-item :label="t('app.name')">
          <el-input v-model="createForm.name" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onCreate">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="assignVisible" :title="t('admin.assignNodes')" width="480px">
      <el-form label-width="100px">
        <el-form-item :label="t('nav.nodes')">
          <el-select v-model="assignForm.nodeId">
            <el-option v-for="n in nodes" :key="n.node_id" :label="n.hostname || n.node_id" :value="n.node_id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('app.actions')">
          <el-switch v-model="assignForm.assign" :active-text="'assign'" :inactive-text="'unassign'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="assignVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onAssign">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="grantUserVisible" :title="t('admin.grantUsers')" width="480px">
      <el-form label-width="100px">
        <el-form-item :label="t('app.username')">
          <el-select v-model="grantUserForm.userId">
            <el-option v-for="u in users" :key="u.id" :label="u.username" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('app.actions')">
          <el-switch v-model="grantUserForm.grant" :active-text="'grant'" :inactive-text="'revoke'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="grantUserVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onGrantUser">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="grantDatasetVisible" :title="t('admin.grantDatasets')" width="480px">
      <el-form label-width="100px">
        <el-form-item :label="t('nav.datasets')">
          <el-select v-model="grantDatasetForm.datasetId">
            <el-option v-for="d in datasets" :key="d.dataset_id" :label="d.name" :value="d.dataset_id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('app.actions')">
          <el-switch v-model="grantDatasetForm.grant" :active-text="'grant'" :inactive-text="'revoke'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="grantDatasetVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onGrantDataset">{{ t("app.confirm") }}</el-button>
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
</style>
