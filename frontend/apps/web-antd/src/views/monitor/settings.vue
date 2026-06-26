<script setup lang="ts">
import type { SettingsRecord } from '#/api';

import { onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import { Button, Card, Form, Input, InputNumber, message } from 'ant-design-vue';

import { createSettings, listSettings, updateSettings } from '#/api';

const loading = ref(false);
const saving = ref(false);
const recordID = ref('');
const form = reactive<Partial<SettingsRecord>>({
  check_interval_minutes: 5,
  telegram_bot_token: '',
  telegram_chat_id: '',
});

async function load() {
  loading.value = true;
  try {
    const resp = await listSettings();
    const first = resp.items[0];
    if (first) {
      recordID.value = first.id;
      Object.assign(form, first);
    }
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const data = {
      check_interval_minutes: Number(form.check_interval_minutes) || 5,
      telegram_bot_token: form.telegram_bot_token || '',
      telegram_chat_id: form.telegram_chat_id || '',
    };
    const saved = recordID.value
      ? await updateSettings(recordID.value, data)
      : await createSettings(data);
    recordID.value = saved.id;
    Object.assign(form, saved);
    message.success('设置已保存');
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <Page title="全局设置">
    <div class="p-4">
      <Card :loading="loading">
        <Form :label-col="{ span: 5 }" :model="form" :wrapper-col="{ span: 14 }">
          <Form.Item label="探测频率(分钟)" required>
            <InputNumber
              v-model:value="form.check_interval_minutes"
              class="w-full"
              :min="1"
              :precision="0"
            />
          </Form.Item>
          <Form.Item label="Telegram Bot Token">
            <Input.Password
              v-model:value="form.telegram_bot_token"
              placeholder="告警 Bot Token"
            />
          </Form.Item>
          <Form.Item label="Telegram Chat ID">
            <Input v-model:value="form.telegram_chat_id" placeholder="告警 Chat ID" />
          </Form.Item>
          <Form.Item :wrapper-col="{ offset: 5, span: 14 }">
            <Button :loading="saving" type="primary" @click="save">保存</Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  </Page>
</template>
