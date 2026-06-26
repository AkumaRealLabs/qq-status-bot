import type { RouteRecordRaw } from 'vue-router';

import { traverseTreeValues } from '@vben/utils';

import { coreRoutes, fallbackNotFoundRoute } from './core';

const externalRoutes: RouteRecordRaw[] = [];
const accessRoutes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:activity',
      order: -1,
      title: '监控后台',
    },
    name: 'Monitor',
    path: '/monitor',
    redirect: '/monitor/status',
    children: [
      {
        name: 'MonitorStatus',
        path: '/monitor/status',
        component: () => import('#/views/monitor/status.vue'),
        meta: {
          affixTab: true,
          icon: 'lucide:gauge',
          title: '状态监控',
        },
      },
      {
        name: 'MonitorBalances',
        path: '/monitor/balances',
        component: () => import('#/views/monitor/balances.vue'),
        meta: {
          icon: 'lucide:wallet',
          title: '余额监控',
        },
      },
      {
        name: 'MonitorOverview',
        path: '/monitor/overview',
        redirect: '/monitor/status',
        meta: {
          hideInMenu: true,
          hideInTab: true,
          title: '总览',
        },
      },
      {
        name: 'MonitorUpstreams',
        path: '/monitor/upstreams',
        component: () => import('#/views/monitor/upstreams.vue'),
        meta: {
          icon: 'lucide:server',
          title: '上游管理',
        },
      },
      {
        name: 'MonitorSettings',
        path: '/monitor/settings',
        component: () => import('#/views/monitor/settings.vue'),
        meta: {
          icon: 'lucide:settings',
          title: '全局设置',
        },
      },
    ],
  },
];

/** 路由列表，由基本路由、外部路由和404兜底路由组成
 *  无需走权限验证（会一直显示在菜单中） */
const routes: RouteRecordRaw[] = [
  ...coreRoutes,
  ...externalRoutes,
  fallbackNotFoundRoute,
];

/** 基本路由列表，这些路由不需要进入权限拦截 */
const coreRouteNames = traverseTreeValues(coreRoutes, (route) => route.name);

export { accessRoutes, coreRouteNames, routes };
