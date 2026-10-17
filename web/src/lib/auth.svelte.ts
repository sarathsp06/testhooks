/**
 * Access-token state for servers that set AUTH_TOKEN.
 *
 * The token lives in localStorage so it survives reloads. When the server
 * answers 401, callers await requireToken(): it opens the sign-in prompt and
 * resolves once a new token has been saved, so the caller can retry.
 */

const AUTH_TOKEN_KEY = 'testhooks_auth_token';

/** Fired on window whenever the token changes, so long-lived connections can reconnect. */
export const AUTH_CHANGED_EVENT = 'testhooks:auth-changed';

function readToken(): string {
	try {
		return localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
	} catch {
		return '';
	}
}

function writeToken(token: string) {
	try {
		if (token) localStorage.setItem(AUTH_TOKEN_KEY, token);
		else localStorage.removeItem(AUTH_TOKEN_KEY);
	} catch {
		// Storage unavailable (private mode, blocked): keep the token in memory only.
	}
}

class AuthState {
	token = $state(readToken());
	/** True while the sign-in prompt should be shown. */
	prompting = $state(false);
	/** Message shown in the prompt, e.g. when a saved token was rejected. */
	error = $state('');

	#waiters: Array<() => void> = [];

	get signedIn(): boolean {
		return this.token !== '';
	}

	/**
	 * Called after a 401. sentToken is the token the failed request used.
	 * Resolves when the caller should retry.
	 */
	requireToken(sentToken: string): Promise<void> {
		// The token changed while the request was in flight: just retry with the new one.
		if (sentToken !== this.token) return Promise.resolve();

		if (sentToken) this.error = 'Your saved access token was rejected. Enter the current one.';
		this.prompting = true;
		return new Promise((resolve) => this.#waiters.push(resolve));
	}

	/** Store a token that the server has accepted, and release waiting requests. */
	save(token: string) {
		writeToken(token);
		this.token = token;
		this.prompting = false;
		this.error = '';
		const waiters = this.#waiters;
		this.#waiters = [];
		waiters.forEach((resolve) => resolve());
		window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
	}

	signOut() {
		writeToken('');
		this.token = '';
		window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
	}
}

export const auth = new AuthState();
