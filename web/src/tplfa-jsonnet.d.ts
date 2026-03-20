// Type declarations for tplfa-jsonnet (Go→WASM Jsonnet evaluator)
// Separate file because app.d.ts is a module (has exports), which turns
// `declare module` into augmentation rather than ambient declaration.

declare module 'tplfa-jsonnet/wasm_exec.js' {
	// Side-effect import that sets up globalThis.Go for Go WASM
	const _: void;
	export default _;
}

declare module 'tplfa-jsonnet/jsonnet.js' {
	export type Jsonnet = {
		evaluate: (
			code: string,
			extrStrs?: Record<string, string>,
			files?: Record<string, string>
		) => Promise<string>;
	};
	export function getJsonnet(
		jnWasm: Promise<Response> | BufferSource
	): Promise<Jsonnet>;
}
