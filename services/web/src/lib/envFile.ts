// plan.md §M6.5: the Environment bulk editor accepts regular `.env` file
// syntax — KEY=value lines, full-line # comments, optional `export ` prefix,
// optional single/double quotes around the value (no escape processing).
// Line-based only: multi-line quoted values are NOT supported, and values
// containing line breaks cannot round-trip through this format — callers
// must protect those keys from the diff (see EnvironmentTab).

export interface EnvParseError {
  line: number; // 1-based
  message: string;
}

export interface EnvParseResult {
  vars: { key: string; value: string }[];
  errors: EnvParseError[];
}

const KEY_RE = /^[A-Z_][A-Z0-9_]*$/;
const RESERVED = new Set(['PORT', 'DATABASE_URL']);
const ASSIGN_RE = /^(?:export\s+)?([^\s=]+)\s*=(.*)$/;
const MAX_VALUE_BYTES = 4 * 1024;

/** Parse `.env` text. Last assignment wins for a repeated key (dotenv behaviour). */
export function parseEnvFile(text: string): EnvParseResult {
  const errors: EnvParseError[] = [];
  const byKey = new Map<string, string>();

  text.split('\n').forEach((raw, i) => {
    const line = i + 1;
    const trimmed = raw.trim();
    if (trimmed === '' || trimmed.startsWith('#')) return;

    const m = ASSIGN_RE.exec(trimmed);
    if (!m) {
      errors.push({ line, message: 'expected KEY=value' });
      return;
    }
    const key = m[1];
    let value = m[2].trim();
    if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
      value = value.slice(1, -1);
    } else if (value.length >= 2 && value.startsWith("'") && value.endsWith("'")) {
      value = value.slice(1, -1);
    }

    if (key.length > 128 || !KEY_RE.test(key)) {
      errors.push({ line, message: 'key must match ^[A-Z_][A-Z0-9_]*$ (max 128 chars)' });
      return;
    }
    if (RESERVED.has(key)) {
      errors.push({ line, message: `${key} is reserved by the platform` });
      return;
    }
    if (value === '') {
      errors.push({ line, message: 'value is empty (1–4096 characters required)' });
      return;
    }
    if (new TextEncoder().encode(value).length > MAX_VALUE_BYTES) {
      errors.push({ line, message: 'value exceeds 4 KB' });
      return;
    }
    byKey.set(key, value); // last wins
  });

  return { vars: [...byKey.entries()].map(([key, value]) => ({ key, value })), errors };
}

/** Serialize current vars back to `.env` text. Multi-line values are skipped
 *  (they can't round-trip line-based parsing) — callers keep them out of the diff. */
export function toEnvFile(vars: { key: string; value: string }[]): { text: string; multiline: string[] } {
  const multiline: string[] = [];
  const lines: string[] = [];
  for (const { key, value } of vars) {
    if (value.includes('\n')) {
      multiline.push(key);
      continue;
    }
    // Quote only when bare text would not round-trip: leading/trailing
    // whitespace, or a value that itself starts+ends with one quote char
    // (the parser strips one matching outer pair).
    const needsQuote =
      value !== value.trim() ||
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"));
    lines.push(needsQuote ? `${key}="${value}"` : `${key}=${value}`);
  }
  return { text: lines.join('\n'), multiline };
}
