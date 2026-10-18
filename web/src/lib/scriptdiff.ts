/** Comparing a request before and after a transform. */
import type { CapturedRequest } from './types';

export type Line = { kind: ' ' | '+' | '-'; text: string };

export function pretty(body: string | null | undefined): string {
	if (!body) return '';
	try {
		return JSON.stringify(JSON.parse(body), null, 2);
	} catch {
		return body;
	}
}

/** Line diff (LCS). Bodies are small; huge ones are shown without markers. */
export function diffLines(a: string, b: string): Line[] {
	const x = a ? a.split('\n') : [];
	const y = b ? b.split('\n') : [];
	if (x.length * y.length > 250_000) return y.map((text) => ({ kind: ' ', text }));
	const dp = Array.from({ length: x.length + 1 }, () => new Array<number>(y.length + 1).fill(0));
	for (let i = x.length - 1; i >= 0; i--)
		for (let j = y.length - 1; j >= 0; j--)
			dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
	const out: Line[] = [];
	let i = 0;
	let j = 0;
	while (i < x.length && j < y.length) {
		if (x[i] === y[j]) {
			out.push({ kind: ' ', text: x[i++] });
			j++;
		} else if (dp[i + 1][j] >= dp[i][j + 1]) out.push({ kind: '-', text: x[i++] });
		else out.push({ kind: '+', text: y[j++] });
	}
	while (i < x.length) out.push({ kind: '-', text: x[i++] });
	while (j < y.length) out.push({ kind: '+', text: y[j++] });
	return out;
}

/** Header, method and path changes, in words. */
export function otherChanges(before: CapturedRequest, after: CapturedRequest): string[] {
	const out: string[] = [];
	if (after.method !== before.method) out.push(`method ${before.method} → ${after.method}`);
	if (after.path !== before.path) out.push(`path ${before.path} → ${after.path}`);
	const lower = (h: Record<string, string[]> | undefined) =>
		new Map(Object.entries(h ?? {}).map(([k, v]) => [k.toLowerCase(), (v ?? []).join(', ')]));
	const a = lower(before.headers);
	const b = lower(after.headers);
	for (const [k, v] of b) {
		if (!a.has(k)) out.push(`+ header ${k}`);
		else if (a.get(k) !== v) out.push(`header ${k} changed`);
	}
	for (const k of a.keys()) if (!b.has(k)) out.push(`− header ${k}`);
	return out;
}

/** Top-level JSON fields added and removed, for the "+1 field, −1 field" summary. */
export function fieldChanges(before: string, after: string): { added: number; removed: number } | null {
	try {
		const a = JSON.parse(before);
		const b = JSON.parse(after);
		if (!a || !b || typeof a !== 'object' || typeof b !== 'object') return null;
		const ka = new Set(Object.keys(a));
		const kb = new Set(Object.keys(b));
		return { added: [...kb].filter((k) => !ka.has(k)).length, removed: [...ka].filter((k) => !kb.has(k)).length };
	} catch {
		return null;
	}
}
