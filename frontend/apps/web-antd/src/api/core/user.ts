import type { UserInfo } from '@vben/types';

/**
 * 获取用户信息
 */
export async function getUserInfoApi() {
  return {
    avatar: '',
    homePath: '/monitor/status',
    realName: 'PocketBase Superuser',
    roles: ['superuser'],
    userId: 'superuser',
    username: 'superuser',
  } as UserInfo;
}
