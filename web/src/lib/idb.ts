/**
 * IndexedDB wrapper for browser-mode request history.
 * Uses the `idb` library by Jake Archibald for a clean Promise-based API.
 *
 * Browser-mode endpoints don't store data on the server. If the user opts in,
 * requests are persisted in IndexedDB locally.
 */
import { openDB, type IDBPDatabase } from 'idb';
import type { CapturedRequest } from './types';

const DB_NAME = 'testhooks';
const DB_VERSION = 1;
const STORE_NAME = 'requests';

let dbPromise: Promise<IDBPDatabase> | null = null;

function getDB(): Promise<IDBPDatabase> {
	if (!dbPromise) {
		dbPromise = openDB(DB_NAME, DB_VERSION, {
			upgrade(db) {
				if (!db.objectStoreNames.contains(STORE_NAME)) {
					const store = db.createObjectStore(STORE_NAME, { keyPath: 'id' });
					store.createIndex('endpoint_id', 'endpoint_id');
					store.createIndex('created_at', 'created_at');
					store.createIndex('endpoint_created', ['endpoint_id', 'created_at']);
				}
			}
		});
	}
	return dbPromise;
}

/** Store a captured request in IndexedDB */
export async function saveRequest(req: CapturedRequest): Promise<void> {
	const db = await getDB();
	await db.put(STORE_NAME, req);
}

/** Get all requests for an endpoint, sorted by created_at DESC */
export async function getRequests(
	endpointId: string,
	limit = 50,
	offset = 0
): Promise<{ requests: CapturedRequest[]; total: number }> {
	const db = await getDB();
	const tx = db.transaction(STORE_NAME, 'readonly');
	const store = tx.objectStore(tx.objectStoreNames[0]);
	const index = store.index('endpoint_id');

	// Count total
	const total = await index.count(endpointId);

	// Get all keys for this endpoint, reverse them (newest first), then slice
	const allKeys = await index.getAllKeys(endpointId);
	const sortedKeys = allKeys.reverse();
	const pageKeys = sortedKeys.slice(offset, offset + limit);

	const requests: CapturedRequest[] = [];
	for (const key of pageKeys) {
		const req = await store.get(key);
		if (req) requests.push(req as CapturedRequest);
	}

	// Sort by created_at DESC
	requests.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

	return { requests, total };
}

/** Delete a single request from IndexedDB */
export async function deleteLocalRequest(id: string): Promise<void> {
	const db = await getDB();
	await db.delete(STORE_NAME, id);
}

/** Delete all requests for an endpoint */
export async function clearLocalRequests(endpointId: string): Promise<void> {
	const db = await getDB();
	const tx = db.transaction(STORE_NAME, 'readwrite');
	const store = tx.objectStore(tx.objectStoreNames[0]);
	const index = store.index('endpoint_id');
	const keys = await index.getAllKeys(endpointId);
	for (const key of keys) {
		await store.delete(key);
	}
	await tx.done;
}

/** Get total count of stored requests for an endpoint */
export async function countLocalRequests(endpointId: string): Promise<number> {
	const db = await getDB();
	const tx = db.transaction(STORE_NAME, 'readonly');
	const index = tx.objectStore(tx.objectStoreNames[0]).index('endpoint_id');
	return index.count(endpointId);
}
