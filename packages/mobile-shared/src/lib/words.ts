/** Grammar every screen shares, so "1 tenancies" cannot be fixed in one app only. */

export function plural(n: number, one: string, many?: string): string {
  return n === 1 ? one : many ?? `${one}s`;
}

export function count(n: number, one: string, many?: string): string {
  return `${n} ${plural(n, one, many)}`;
}
