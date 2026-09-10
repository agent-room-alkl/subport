import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from '../api/client'
import Shell from '../layouts/Shell.vue'
import ConsoleShell from '../layouts/ConsoleShell.vue'
import Login from '../views/Login.vue'
import Overview from '../views/Overview.vue'
import Accounts from '../views/Accounts.vue'
import Channels from '../views/Channels.vue'
import Proxies from '../views/Proxies.vue'
import Keys from '../views/Keys.vue'
import Usage from '../views/Usage.vue'
import Routes from '../views/Routes.vue'
import Recharge from '../views/Recharge.vue'
import ConsoleDashboard from '../views/console/Dashboard.vue'
import ConsoleRecharge from '../views/console/Recharge.vue'
import ConsoleKeys from '../views/console/Keys.vue'
import ConsoleUsage from '../views/console/Usage.vue'
import ConsoleProfile from '../views/console/Profile.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login, meta: { public: true } },
    {
      path: '/console',
      component: ConsoleShell,
      children: [
        { path: '', name: 'console-home', component: ConsoleDashboard },
        { path: 'recharge', name: 'console-recharge', component: ConsoleRecharge },
        { path: 'keys', name: 'console-keys', component: ConsoleKeys },
        { path: 'usage', name: 'console-usage', component: ConsoleUsage },
        { path: 'profile', name: 'console-profile', component: ConsoleProfile },
      ],
    },
    {
      path: '/',
      component: Shell,
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', name: 'overview', component: Overview },
        { path: 'accounts', name: 'accounts', component: Accounts },
        { path: 'channels', name: 'channels', component: Channels },
        { path: 'routes', name: 'routes', component: Routes },
        { path: 'proxies', name: 'proxies', component: Proxies },
        { path: 'keys', name: 'keys', component: Keys },
        { path: 'usage', name: 'usage', component: Usage },
        { path: 'recharge', name: 'recharge', component: Recharge, meta: { admin: true } },
      ],
    },
  ],
})

router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!getToken()) return { name: 'login', query: { redirect: to.fullPath } }
  try {
    const raw = localStorage.getItem('subport_user')
    const user = raw ? JSON.parse(raw) : null
    if (to.meta.admin && user?.role && user.role !== 'admin') {
      return { name: 'console-home' }
    }
  } catch { /* ignore */ }
  return true
})

export default router
