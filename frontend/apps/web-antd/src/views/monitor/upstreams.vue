<script setup lang="ts">
import type { UpstreamRecord } from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  message,
} from 'ant-design-vue';

import {
  browserCapture,
  browserLogin,
  createUpstream,
  listUpstreams,
  updateUpstream,
} from '#/api';

type FormState = Partial<UpstreamRecord>;

const rows = ref<UpstreamRecord[]>([]);
const loading = ref(false);
const saving = ref(false);
const open = ref(false);
const editingID = ref('');
const acting = ref('');
const form = reactive<FormState>(blankForm());

const columns = [
  { dataIndex: 'name', title: '名称' },
  { dataIndex: 'type', title: '类型', width: 110 },
  { dataIndex: 'base_url', title: '地址' },
  { dataIndex: 'enabled', title: '状态', width: 100 },
  { dataIndex: 'balance_rate', title: '余额倍率', width: 120 },
  { dataIndex: 'low_balance_threshold', title: '低余额阈值(RMB)', width: 150 },
  { dataIndex: 'actions', title: '操作', width: 280 },
];

const title = computed(() => (editingID.value ? '编辑上游' : '新增上游'));

function blankForm(): FormState {
  return {
    access_token: '',
    base_url: '',
    balance_rate: 1,
    enabled: true,
    low_balance_threshold: 0,
    name: '',
    type: 'newapi',
    user_id: '',
  };
}

function resetForm(data?: UpstreamRecord) {
  Object.assign(form, blankForm(), data || {});
  if (!form.balance_rate || Number(form.balance_rate) <= 0) {
    form.balance_rate = 1;
  }
  editingID.value = data?.id || '';
}

async function load() {
  loading.value = true;
  try {
    rows.value = (await listUpstreams()).items;
  } finally {
    loading.value = false;
  }
}

function add() {
  resetForm();
  open.value = true;
}

function edit(record: UpstreamRecord) {
  resetForm(record);
  open.value = true;
}

function payload() {
  const data: Partial<UpstreamRecord> = {
    base_url: form.base_url || '',
    balance_rate: Number(form.balance_rate || 1),
    enabled: !!form.enabled,
    low_balance_threshold: Number(form.low_balance_threshold || 0),
    name: form.name || '',
    type: form.type || 'newapi',
  };
  if (data.type === 'newapi') {
    data.access_token = form.access_token || '';
    data.user_id = form.user_id || '';
  }
  return data;
}

async function save() {
  if (!form.name || !form.base_url) {
    message.error('请填写名称和地址');
    return;
  }
  saving.value = true;
  try {
    const data = payload();
    if (editingID.value) {
      await updateUpstream(editingID.value, data);
    } else {
      await createUpstream(data);
    }
    message.success('上游已保存');
    open.value = false;
    await load();
  } finally {
    saving.value = false;
  }
}

async function toggle(record: UpstreamRecord, enabled: boolean) {
  await updateUpstream(record.id, { enabled });
  message.success(enabled ? '已启用' : '已停用');
  await load();
}

async function openBrowser(record: UpstreamRecord) {
  acting.value = `browser:${record.id}`;
  try {
    const info = await browserLogin(record.id);
    if (info.vnc_url) {
      window.open(info.vnc_url, '_blank');
    }
    message.success('浏览器已打开，登录后点抓取凭据');
  } finally {
    acting.value = '';
  }
}

async function captureCredentials(record: UpstreamRecord) {
  acting.value = `capture:${record.id}`;
  try {
    const info = await browserCapture(record.id);
    message.success(
      `已写入凭据：access=${info.access_token ? 'yes' : 'no'} refresh=${
        info.refresh_token ? 'yes' : 'no'
      }`,
    );
    await load();
  } finally {
    acting.value = '';
  }
}

function editRow(record: Record<string, any>) {
  edit(record as UpstreamRecord);
}

function toggleRow(record: Record<string, any>, checked: unknown) {
  return toggle(record as UpstreamRecord, !!checked);
}

function openBrowserRow(record: Record<string, any>) {
  return openBrowser(record as UpstreamRecord);
}

function captureCredentialsRow(record: Record<string, any>) {
  return captureCredentials(record as UpstreamRecord);
}

onMounted(load);
</script>

<template>
  <Page title="上游管理">
    <div class="p-4">
      <Card>
        <template #extra>
          <Button type="primary" @click="add">新增上游</Button>
        </template>
        <Table
          :columns="columns"
          :data-source="rows"
          :loading="loading"
          :pagination="{ pageSize: 20 }"
          row-key="id"
          :scroll="{ x: 1000 }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'name'">
              <div class="font-medium">{{ record.name }}</div>
              <div class="text-xs text-gray-500">
                {{ record.base_url }}
              </div>
            </template>
            <template v-else-if="column.dataIndex === 'type'">
              <Tag>{{ record.type }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'base_url'">
              <span class="break-all">{{ record.base_url }}</span>
            </template>
            <template v-else-if="column.dataIndex === 'enabled'">
              <Switch
                :checked="record.enabled"
                size="small"
                @change="(checked) => toggleRow(record, checked)"
              />
            </template>
            <template v-else-if="column.dataIndex === 'balance_rate'">
              {{ record.balance_rate || 1 }}
            </template>
            <template v-else-if="column.dataIndex === 'actions'">
              <Space wrap>
                <Button size="small" @click="editRow(record)">编辑</Button>
                <Button
                  v-if="record.type === 'sub2api'"
                  :loading="acting === `browser:${record.id}`"
                  size="small"
                  @click="openBrowserRow(record)"
                >
                  浏览器登录
                </Button>
                <Button
                  v-if="record.type === 'sub2api'"
                  :loading="acting === `capture:${record.id}`"
                  size="small"
                  @click="captureCredentialsRow(record)"
                >
                  抓取凭据
                </Button>
              </Space>
            </template>
          </template>
        </Table>
      </Card>

      <Modal
        v-model:open="open"
        :confirm-loading="saving"
        :title="title"
        width="720px"
        @ok="save"
      >
        <Form :label-col="{ span: 6 }" :model="form" :wrapper-col="{ span: 16 }">
          <Form.Item label="类型" required>
            <Select v-model:value="form.type">
              <Select.Option value="newapi">newapi</Select.Option>
              <Select.Option value="sub2api">sub2api</Select.Option>
            </Select>
          </Form.Item>
          <Form.Item label="名称" required>
            <Input v-model:value="form.name" />
          </Form.Item>
          <Form.Item label="地址" required>
            <Input v-model:value="form.base_url" placeholder="https://example.com" />
          </Form.Item>
          <Form.Item label="启用">
            <Switch v-model:checked="form.enabled" />
          </Form.Item>
          <Form.Item label="余额倍率">
            <InputNumber
              v-model:value="form.balance_rate"
              class="w-full"
              :min="0.0001"
              :precision="4"
              :step="0.1"
              addon-after="RMB / USD"
              placeholder="比如 0.1"
            />
          </Form.Item>
          <Form.Item label="低余额阈值(RMB)">
            <InputNumber
              v-model:value="form.low_balance_threshold"
              class="w-full"
              :min="0"
            />
          </Form.Item>

          <template v-if="form.type === 'newapi'">
            <Form.Item label="User ID">
              <Input v-model:value="form.user_id" />
            </Form.Item>
            <Form.Item label="Access Token">
              <Input.Password v-model:value="form.access_token" />
            </Form.Item>
          </template>

        </Form>
      </Modal>
    </div>
  </Page>
</template>
