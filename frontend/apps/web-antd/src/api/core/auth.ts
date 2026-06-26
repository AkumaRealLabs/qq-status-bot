import { requestClient } from '#/api/request';

export namespace AuthApi {
  /** 登录接口参数 */
  export interface LoginParams {
    password?: string;
    username?: string;
  }

  /** 登录接口返回值 */
  export interface LoginResult {
    accessToken: string;
  }

  export interface RefreshTokenResult {}
}

/**
 * 登录
 */
export async function loginApi(data: AuthApi.LoginParams) {
  const resp = await requestClient.post<{ token: string }>(
    '/collections/_superusers/auth-with-password',
    {
      identity: data.username,
      password: data.password,
    },
  );
  return { accessToken: resp.token };
}

/**
 * 刷新accessToken
 */
export async function refreshTokenApi() {
  return {};
}

/**
 * 退出登录
 */
export async function logoutApi() {
  return {};
}

/**
 * 获取用户权限码
 */
export async function getAccessCodesApi() {
  return ['superuser'];
}
