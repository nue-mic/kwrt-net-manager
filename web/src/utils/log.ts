// 日志行「展示净化」：仅在前端渲染前剥掉两段对单实例视图无意义、且白白
// 占据横向空间的噪音。注意——只动展示，后端日志文件与核心逻辑一律不碰。
//
// 1) `[inst=<id>]`：kwrtmgrd 在 svc.Run 前往 xlog 注入的实例前缀（见
//    services/instance_context.go）。它的用途是「多个 frpc 实例合并写日志时
//    按实例分流」——属于核心业务逻辑，后端必须保留。但前端两个日志视图
//    （配置详情·运行日志速览 / 实时日志流监控）永远是「已选定单个实例」的
//    上下文，每行再重复 inst=<id> 纯属冗余，故仅在前端剥离。
// 2) `[<16位hex>]`：frp 为每条连接会话生成的 runID（如 00b42428887e954b）。
//    对普通用户排障价值很低，却占一截宽度，一并剥掉。
//
// 设计取舍：
// - 前导 `\s*` 一起吞掉分隔空格，避免剥离后留下双空格。
// - runID 严格匹配 16 位 hex，宁可漏剥也不误删 message 里的普通方括号内容。
// - 不影响行级别着色：级别标记 [D]/[I]/[W]/[E] 不落在这两段内。

const INST_PREFIX = /\s*\[inst=[^\]]*\]/g;
const RUN_ID = /\s*\[[0-9a-f]{16}\]/gi;

/** 剥掉日志行里的 `[inst=<id>]` 与 `[<runID>]` 两段噪音，仅用于前端展示。 */
export function stripLogNoise(line: string): string {
  if (!line) return line;
  return line.replace(INST_PREFIX, '').replace(RUN_ID, '');
}
