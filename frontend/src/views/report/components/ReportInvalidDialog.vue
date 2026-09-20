<template>
  <el-dialog
    :model-value="modelValue"
    title="核实无效"
    width="520px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-alert type="warning" :closable="false" show-icon class="invalid-tip">
      判定为无效报修时, 必须填写原因, 便于向市民反馈与后续追溯。
    </el-alert>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
      <el-form-item label="报修单号">
        <span>{{ report?.report_no }}</span>
      </el-form-item>
      <el-form-item label="无效原因" prop="reason">
        <el-select
          v-model="form.reason"
          allow-create
          filterable
          default-first-option
          placeholder="选择或输入无效原因"
          style="width: 100%"
        >
          <el-option v-for="item in reasonPresets" :key="item" :label="item" :value="item" />
        </el-select>
      </el-form-item>
      <el-form-item label="核实人" prop="verified_by">
        <el-input v-model="form.verified_by" placeholder="现场核实人员" />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="danger" :loading="submitting" @click="handleSubmit">确认无效</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { reportApi } from '@/api/report'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  report: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue', 'verified'])

const reasonPresets = [
  '现场核实路灯运行正常, 报修情况不存在',
  '非市政路灯(小区 / 园区自建照明), 转产权单位处理',
  '位置描述无法定位到具体路灯',
  '计划检修 / 远程试灯, 灯具运行正常',
  '重复反映同一问题, 已并入其它报修单',
]

const formRef = ref(null)
const submitting = ref(false)

const createForm = () => ({ reason: '', verified_by: '' })
const form = reactive(createForm())

const rules = {
  reason: [{ required: true, message: '请填写无效原因', trigger: 'change' }],
}

function syncForm() {
  Object.assign(form, createForm())
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    await reportApi.invalid(props.report.id, { ...form })
    emit('update:modelValue', false)
    emit('verified')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.invalid-tip {
  margin-bottom: 16px;
}
</style>
