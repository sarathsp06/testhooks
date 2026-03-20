import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** Merge Tailwind classes conditionally */
export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

/** Format bytes to human-readable string */
export function formatBytes(bytes: number): string {
	if (bytes === 0) return '0 B';
	const sizes = ['B', 'KB', 'MB'];
	const i = Math.floor(Math.log(bytes) / Math.log(1024));
	return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${sizes[i]}`;
}

/** Format ISO date string to relative time or short date */
export function formatTime(iso: string): string {
	const date = new Date(iso);
	const now = Date.now();
	const diff = now - date.getTime();

	if (diff < 60_000) return 'just now';
	if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
	if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
	return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

/** Format ISO date to a full datetime string */
export function formatFullTime(iso: string): string {
	return new Date(iso).toLocaleString('en-US', {
		year: 'numeric',
		month: 'short',
		day: 'numeric',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});
}

/** HTTP method -> color class */
export function methodColor(method: string): string {
	const colors: Record<string, string> = {
		GET: 'text-green-400',
		POST: 'text-blue-400',
		PUT: 'text-yellow-400',
		PATCH: 'text-orange-400',
		DELETE: 'text-red-400',
		HEAD: 'text-purple-400',
		OPTIONS: 'text-gray-400'
	};
	return colors[method.toUpperCase()] || 'text-gray-400';
}

/** Try to pretty-print JSON, returns original string if not valid JSON */
export function tryFormatJSON(str: string): { formatted: string; isJSON: boolean } {
	try {
		const parsed = JSON.parse(str);
		return { formatted: JSON.stringify(parsed, null, 2), isJSON: true };
	} catch {
		return { formatted: str, isJSON: false };
	}
}

/** Get the full webhook URL for a slug */
export function getWebhookURL(slug: string): string {
	return `${window.location.origin}/h/${slug}`;
}

/** Copy text to clipboard */
export async function copyToClipboard(text: string): Promise<boolean> {
	try {
		await navigator.clipboard.writeText(text);
		return true;
	} catch {
		return false;
	}
}
