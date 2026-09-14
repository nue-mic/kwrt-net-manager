/** 表格排序用的比较器：IP 按数值比，文本按中文规则比。 */

/** IPv4 转 32 位整数；非法/非 IPv4 返回 -1（排在最前）。 */
export function ipToNum(ip: string): number {
  const parts = (ip || '').split('.');
  if (parts.length !== 4) return -1;
  let n = 0;
  for (const p of parts) {
    const v = Number(p);
    if (p === '' || !Number.isInteger(v) || v < 0 || v > 255) return -1;
    n = n * 256 + v;
  }
  return n;
}

/** 按 IPv4 数值比较（字符串比较会把 .9 排到 .100 后面）。 */
export const cmpIp = (a: string, b: string) => ipToNum(a) - ipToNum(b);

/** 按文本比较（中文按拼音）。 */
export const cmpText = (a: string, b: string) => (a || '').localeCompare(b || '', 'zh');
