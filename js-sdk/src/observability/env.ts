/**
 * Shared env helpers for config parity with Go and Python SDKs.
 */

export function getEnv(key: string, defaultValue: string): string {
  if (key === 'ENVIRONMENT') {
    for (const k of ['ENVIRONMENT', 'ENV', 'environment', 'env']) {
      const v = process.env[k];
      if (v) return v;
    }
  }
  const value = process.env[key];
  return value !== undefined && value !== '' ? value : defaultValue;
}

export function getEnvBool(key: string, defaultValue: boolean): boolean {
  const value = process.env[key];
  if (value === undefined || value === '') return defaultValue;
  const lower = value.toLowerCase();
  return lower === 'true' || lower === '1' || lower === 'yes';
}

export function getEnvNumber(key: string, defaultValue: number): number {
  const value = process.env[key];
  if (value === undefined || value === '') return defaultValue;
  const n = parseFloat(value);
  return Number.isFinite(n) ? n : defaultValue;
}
