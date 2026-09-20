<template>
  <el-dialog
    :model-value="modelValue"
    title="核实有效 · 转正式故障"
    width="640px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-alert type="success" :closable="false" show-icon class="verify-tip">
      核实通过后将生成正式故障单, 来源标记为「市民上报」, 并保留市民原始描述。
    </el-alert>

    <el-descriptions :column="1" border size="small" class="origin-box">
      <el-descriptions-item label="报修单号">{{ report?.report_no }}</el-descriptions-item>
      <el-descriptions-item label="市民原始描述">
        <span class="origin-content">{{ report?.content }}</span>
      </el-descriptions-item>
      <el-descriptions-item label="报修人">
        {{ report?.reporter || '-' }}<span v-if="report?.reporter_phone"> · {{ report.reporter_phone }}</span>
      </el-descriptions-item>
      <el-descriptions-item v-if="report?.duplicate_count" label="重复上报">
        <el-tag type="warning" size="small">已合并 {{ report.duplicate_count }} 条同灯重复上报, 将一并关联</el-tag>
      </el-descriptions-item>
    </el-descriptions>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px" class="verify-form">
      <el-form-item label="核实路灯" prop="lamp_id">
        <el-select
          v-model="form.lamp_id"
          filterable
          remote
          reserve-keyword
          :remote-method="searchLamps"
          :loading="lampLoading"
          placeholder="输入路灯编号 / 道路名称搜索核实"
          style="width: 100%"
          @change="handleLampChange"
        >
          <el-option
            v-for="item in lampCandidates"
            :key="item.id"
            :label="`${item.code} · ${item.road_name} · ${item.name || '未命名'}`"
            :value="item.id"
          />
        </el-select>
        <div v-if="selectedLamp" class="form-hint text-muted">
          当前运行状态: {{ dictLabel(RUN_STATUS, selectedLamp.run_status) }}
        </div>
      </el-form-item>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="故障类型" prop="fault_type">
            <el-select v-model="form.fault_type" placeholder="默认为报修类型" style="width: 100%">
              <el-option v-for="item in faultTypeOptions" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="紧急程度" prop="fault_level">
            <el-select v-model="form.fault_level" style="width: 100%">
              <el-option v-for="(item, key) in FAULT_LEVEL" :key="key" :label="item.label" :value="key" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="24">
          <el-form-item label="核实人" prop="verified_by">
            <el-input v-model="form.verified_by" placeholder="现场核实人员" />
          </el-form-item>
        </el-col>
        <el-col :span="24">
          <el-form-item label="核实备注" prop="verify_remark">
            <el-input
              v-model="form.verify_remark"
              type="textarea"
              :rows="2"
              maxlength="255"
              show-word-limit
              placeholder="现场复核情况, 选填"
            />
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="success" :loading="submitting" @click="handleSubmit">确认有效并转故障</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { reportApi } from '@/api/report'
import { lampApi } from '@/api/lamp'
import { FAULT_LEVEL, RUN_STATUS, dictLabel } from '@/constants/dict'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  report: { type: Object, default: null },
  faultTypeOptions: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'verified'])

const formRef = ref(null)
const submitting = ref(false)
const lampLoading = ref(false)
const lampCandidates = ref([])
const cachedLamp = ref(null)

const selectedLamp = computed(() => cachedLamp.value)

const createForm = () => ({
  lamp_id: undefined,
  fault_type: '',
  fault_level: 'normal',
  verified_by: '',
  verify_remark: '',
})

const form = reactive(createForm())

const rules = {
  lamp_id: [{ required: true, message: '请选择核实的路灯', trigger: 'change' }],
}

async function searchLamps(keyword = '') {
  lampLoading.value = true
  try {
    const data = await lampApi.list({ keyword, page: 1, page_size: 20 }, { silent: true })
    lampCandidates.value = data?.items ?? []
  } catch (error) {
    lampCandidates.value = []
  } finally {
    lampLoading.value = false
  }
}

function handleLampChange(id) {
  cachedLamp.value = lampCandidates.value.find((item) => item.id === id) ?? null
}

async function syncForm() {
  Object.assign(form, {
    ...createForm(),
    fault_type: props.report?.fault_type ?? '',
  })
  cachedLamp.value = null

  await searchLamps('')
  if (props.report?.lamp_id) {
    form.lamp_id = props.report.lamp_id
    try {
      cachedLamp.value = await lampApi.detail(props.report.lamp_id, { silent: true })
      if (cachedLamp.value && !lampCandidates.value.some((item) => item.id === cachedLamp.value.id)) {
        lampCandidates.value = [cachedLamp.value, ...lampCandidates.value]
      }
    } catch (error) {
      cachedLamp.value = null
    }
  }
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    await reportApi.verify(props.report.id, { ...form })
    emit('update:modelValue', false)
    emit('verified')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.verify-tip {
  margin-bottom: 16px;
}

.origin-box {
  margin-bottom: 16px;
}

.origin-content {
  white-space: pre-wrap;
  word-break: break-all;
}

.form-hint {
  font-size: 12px;
  line-height: 1.6;
}
</style>
