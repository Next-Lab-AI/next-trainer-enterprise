import { createRouter, createWebHistory } from "vue-router"
import type { RouteRecordRaw } from "vue-router"

import { getAuthToken } from "./api/client"
import { i18n } from "./i18n"

const LoginPage = () => import("./pages/LoginPage.vue")
const NodesPage = () => import("./pages/NodesPage.vue")
const NodeDetailPage = () => import("./pages/NodeDetailPage.vue")
const JobsPage = () => import("./pages/JobsPage.vue")
const JobDetailPage = () => import("./pages/JobDetailPage.vue")
const JobSubmitPage = () => import("./pages/JobSubmitPage.vue")
const DatasetsPage = () => import("./pages/DatasetsPage.vue")
const ArtifactsPage = () => import("./pages/ArtifactsPage.vue")
const AdminUsersPage = () => import("./pages/AdminUsersPage.vue")
const AdminPoolsPage = () => import("./pages/AdminPoolsPage.vue")
const NotFoundPage = () => import("./pages/NotFoundPage.vue")

const routes: RouteRecordRaw[] = [
  { path: "/login", name: "login", component: LoginPage, meta: { titleKey: "login.title", public: true } },
  { path: "/", redirect: "/nodes" },
  { path: "/nodes", name: "nodes", component: NodesPage, meta: { titleKey: "nav.nodes" } },
  { path: "/nodes/:id", name: "node-detail", component: NodeDetailPage, meta: { titleKey: "nodes.detail" } },
  { path: "/jobs", name: "jobs", component: JobsPage, meta: { titleKey: "nav.jobs" } },
  { path: "/jobs/submit", name: "job-submit", component: JobSubmitPage, meta: { titleKey: "nav.submitJob" } },
  { path: "/jobs/:id", name: "job-detail", component: JobDetailPage, meta: { titleKey: "jobs.detail" } },
  { path: "/datasets", name: "datasets", component: DatasetsPage, meta: { titleKey: "nav.datasets" } },
  { path: "/artifacts", name: "artifacts", component: ArtifactsPage, meta: { titleKey: "nav.artifacts" } },
  { path: "/admin/users", name: "admin-users", component: AdminUsersPage, meta: { titleKey: "nav.adminUsers" } },
  { path: "/admin/pools", name: "admin-pools", component: AdminPoolsPage, meta: { titleKey: "nav.adminPools" } },
  { path: "/:pathMatch(.*)*", component: NotFoundPage, meta: { titleKey: "notFound.title", public: true } },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach((to) => {
  if (to.meta.public) {
    return true
  }
  if (!getAuthToken()) {
    return { name: "login", query: { redirect: to.fullPath } }
  }
  return true
})

router.afterEach((route) => {
  const titleKey = typeof route.meta.titleKey === "string" ? route.meta.titleKey : "app.brand"
  document.title = `${i18n.global.t(titleKey)} | ${i18n.global.t("app.brand")}`
})

export default router
