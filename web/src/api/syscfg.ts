import client from './client';

/**
 * 系统运行时配置 —— 字段对齐后端 internal/api/syscfg.go。
 * effective: 当前生效值（env 默认 ∪ meta.json 覆盖）
 * env_default: KWRTNET_* 环境变量的原始默认值
 * overridden: 各字段是否被 UI 覆盖（true=已固定为 UI 值, false=跟随 env）
 */
export interface SysConfigValues {
  log_level: string;
  self_update_enabled: boolean;
  docs_enabled: boolean;
  cors_origins: string[];
}

export interface SysConfigResp {
  effective: SysConfigValues;
  env_default: SysConfigValues;
  overridden: Record<keyof SysConfigValues, boolean>;
}

/** PUT 入参：提供的字段写为覆盖；reset 里列出的字段清除覆盖、回退 env 默认。 */
export interface SysConfigPatch {
  log_level?: string;
  self_update_enabled?: boolean;
  docs_enabled?: boolean;
  cors_origins?: string[];
  reset?: Array<keyof SysConfigValues>;
}

export async function getSysConfig(): Promise<SysConfigResp> {
  const resp = await client.get<SysConfigResp>('/api/v1/system/config');
  return resp.data;
}

export async function updateSysConfig(patch: SysConfigPatch): Promise<SysConfigResp> {
  const resp = await client.put<SysConfigResp>('/api/v1/system/config', patch);
  return resp.data;
}
