<template>
  <el-dialog
    :model-value="modelValue"
    title="市民报修登记"
    width="640px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-alert type="info" :closable="false" show-icon class="submit-tip">
      市民可依据灯杆标识上的编号报修; 看不清编号时, 填写道路与位置描述也可以受理。短时间内同一路灯的重复上报会自动合并。
    </el-alert>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="灯杆编号" prop="lamp_code">
        <el-input v-model="form.lamp_code" placeholder="标识牌上的编号, 例如 LD-00015" clearable />
      </el-form-item>
      <el-form-item label="所在道路" prop="road_name">
        <el-input v-model="form.road_name" placeholder="无编号时必填, 例如 解放路" />
      </el-form-item>
      <el-form-item label="位置描述" prop="location_desc">
        <el-input
          v-model="form.location_desc"
          type="textarea"
          :rows="2"
          maxlength="255"
          show-word-limit
          placeholder="无编号时必填, 例如 解放路与迎宾大道交叉口东南角斑马线旁"
        />
      </el-form-item>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="故障类型" prop="fault_type">
            <el-select v-model="form.fault_type" placeholder="请选择故障类型" style="width: 100%">
              <el-option v-for="item in faultTypeOptions" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="上报时间" prop="reported_at">
            <el-date-picker
              v-model="form.reported_at"
              type="datetime"
              value-format="YYYY-MM-DD HH:mm:ss"
              placeholder="默认取当前时间"
              style="width: 100%"
            />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="报修人" prop="reporter">
            <el-input v-model="form.reporter" placeholder="市民姓名 / 称呼" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="联系电话" prop="reporter_phone">
            <el-input v-model="form.reporter_phone" placeholder="选填" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item label="报修描述" prop="content">
        <el-input
          v-model="form.content"
          type="textarea"
          :rows="3"
          maxlength="512"
          show-word-limit
          placeholder="市民反映的原始情况, 将原样保留, 例如: 连着两晚不亮, 旁边的灯是好的"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">提交报修</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { reportApi } from '@/api/report'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  faultTypeOptions: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)

const createForm = () => ({
  lamp_code: '',
  road_name: '',
  location_desc: '',
  fault_type: '',
  content: '',
  reporter: '',
  reporter_phone: '',
  reported_at: '',
})

const form = reactive(createForm())

const validateLocation = (_rule, _value, callback) => {
  if (!form.lamp_code.trim() && (!form.road_name.trim() || !form.location_desc.trim())) {
    callback(new Error('请填写灯杆编号, 或同时填写所在道路与位置描述'))
    return
  }
  callback()
}

const rules = {
  lamp_code: [{ validator: validateLocation, trigger: 'change' }],
  road_name: [{ validator: validateLocation, trigger: 'blur' }],
  location_desc: [{ validator: validateLocation, trigger: 'blur' }],
  fault_type: [{ required: true, message: '请选择故障类型', trigger: 'change' }],
  content: [{ required: true, message: '请填写报修描述', trigger: 'blur' }],
}

function syncForm() {
  Object.assign(form, createForm())
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload = { ...form }
    if (!payload.reported_at) delete payload.reported_at
    const result = await reportApi.submit(payload)
    if (result?.merged) {
      ElMessage.success('该路灯短时间内已有报修, 本次上报已自动合并')
    } else {
      ElMessage.success('报修已提交, 进入待核实队列')
    }
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.submit-tip {
  margin-bottom: 16px;
}
</style>
