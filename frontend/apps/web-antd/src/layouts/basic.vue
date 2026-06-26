<script lang="ts" setup>
import { computed } from 'vue';

import { BasicLayout, UserDropdown } from '@vben/layouts';
import { preferences, usePreferences } from '@vben/preferences';
import { useUserStore } from '@vben/stores';

import { useAuthStore } from '#/store';

const userStore = useUserStore();
const authStore = useAuthStore();
usePreferences();

const menus = computed(() => [
  {
    handler: () => {
      window.open('/_/', '_blank');
    },
    icon: 'lucide:database',
    text: 'PocketBase 后台',
  },
]);

const avatar = computed(() => {
  return userStore.userInfo?.avatar ?? preferences.app.defaultAvatar;
});

async function handleLogout() {
  await authStore.logout(false);
}
</script>

<template>
  <BasicLayout @clear-preferences-and-logout="handleLogout">
    <template #user-dropdown>
      <UserDropdown
        :avatar
        :menus
        :text="userStore.userInfo?.realName"
        description="PocketBase Superuser"
        @logout="handleLogout"
        @clear-preferences-and-logout="handleLogout"
      />
    </template>
  </BasicLayout>
</template>
