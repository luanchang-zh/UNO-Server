// 玩家 ID 为雪花 int64，超出 JS Number 安全整数范围，
// 必须在 JSON.parse 之前加引号转为字符串、发送时再还原为原始数字字面量。

/** 把 15 位以上的整数字面量加引号，避免 JSON.parse 丢失精度。 */
export function parseSafeJson<T>(text: string): T {
  const quoted = text.replace(/([:[,]\s*)(\d{15,})(?=\s*[,}\]])/g, '$1"$2"')
  return JSON.parse(quoted) as T
}

/** 把字符串形式的 player_id 还原为 JSON 数字字面量供后端按 int64 解析。 */
export function stringifyWithIds(value: unknown): string {
  return JSON.stringify(value).replace(/"player_id":"(\d+)"/g, '"player_id":$1')
}
