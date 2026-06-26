<script lang="ts" setup>
import type { VbenFormSchema } from '@vben/common-ui';

import { computed } from 'vue';

import { AuthenticationLogin, z } from '@vben/common-ui';

import { useAuthStore } from '#/store';

defineOptions({ name: 'Login' });

const authStore = useAuthStore();

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      component: 'VbenInput',
      componentProps: {
        placeholder: 'Superuser 邮箱或用户名',
      },
      fieldName: 'username',
      label: '账号',
      rules: z.string().min(1, { message: '请输入 Superuser 账号' }),
    },
    {
      component: 'VbenInputPassword',
      componentProps: {
        placeholder: 'Superuser 密码',
      },
      fieldName: 'password',
      label: '密码',
      rules: z.string().min(1, { message: '请输入 Superuser 密码' }),
    },
  ];
});
</script>

<template>
  <AuthenticationLogin
    :form-schema="formSchema"
    :loading="authStore.loginLoading"
    submit-button-text="登录"
    sub-title="使用 PocketBase Superuser 登录"
    title="AI 上游监控"
    :show-code-login="false"
    :show-forget-password="false"
    :show-qrcode-login="false"
    :show-register="false"
    :show-third-party-login="false"
    @submit="authStore.authLogin"
  />
</template>
