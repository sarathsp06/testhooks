/**
 * Guess a TypeScript type or a JSON Schema from one JSON body.
 * One request is one example, so the result is a best-effort starting point.
 */

type Prim = 'string' | 'integer' | 'number' | 'boolean' | 'null';

/** What we saw at one place in the JSON, merged across array elements. */
interface Shape {
	prims: Set<Prim>;
	/** Set when an object was seen here. */
	obj?: { seen: number; fields: Map<string, { shape: Shape; count: number }> };
	/** Set when an array was seen here; `items` is null while every array was empty. */
	arr?: { items: Shape | null };
}

const empty = (): Shape => ({ prims: new Set() });

function observe(shape: Shape, v: unknown): Shape {
	if (v === null) shape.prims.add('null');
	else if (typeof v === 'string') shape.prims.add('string');
	else if (typeof v === 'boolean') shape.prims.add('boolean');
	else if (typeof v === 'number') shape.prims.add(Number.isInteger(v) ? 'integer' : 'number');
	else if (Array.isArray(v)) {
		shape.arr ??= { items: null };
		for (const el of v) shape.arr.items = observe(shape.arr.items ?? empty(), el);
	} else if (typeof v === 'object') {
		shape.obj ??= { seen: 0, fields: new Map() };
		shape.obj.seen++;
		for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
			const f = shape.obj.fields.get(k) ?? { shape: empty(), count: 0 };
			f.count++;
			f.shape = observe(f.shape, val);
			shape.obj.fields.set(k, f);
		}
	}
	return shape;
}

export function infer(value: unknown): Shape {
	return observe(empty(), value);
}

function pascal(s: string): string {
	const out = s
		.split(/[^A-Za-z0-9]+/)
		.filter(Boolean)
		.map((w) => w[0].toUpperCase() + w.slice(1))
		.join('');
	return /^[0-9]/.test(out) ? `T${out}` : out;
}

/** "invoice.paid" -> "InvoicePaidPayload"; anything unusable -> "WebhookPayload". */
export function typeName(event?: string): string {
	const p = event ? pascal(event) : '';
	return p ? `${p}Payload` : 'WebhookPayload';
}

const IDENT = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

/** TypeScript interfaces for the value, root interface first. */
export function toTypeScript(value: unknown, name = 'WebhookPayload'): string {
	const blocks: string[] = [];
	const used = new Set<string>();
	const unique = (n: string) => {
		let out = n || 'Item';
		for (let i = 2; used.has(out); i++) out = `${n}${i}`;
		used.add(out);
		return out;
	};

	function iface(n: string, obj: NonNullable<Shape['obj']>): string {
		const own = unique(n);
		const idx = blocks.push('') - 1; // reserve the slot so parents come before children
		const lines = [...obj.fields].map(([k, f]) => {
			const key = IDENT.test(k) ? k : JSON.stringify(k);
			const opt = f.count < obj.seen ? '?' : '';
			return `  ${key}${opt}: ${ts(f.shape, own + pascal(k))};`;
		});
		blocks[idx] = `export interface ${own} {\n${lines.join('\n')}${lines.length ? '\n' : ''}}`;
		return own;
	}

	function ts(s: Shape, hint: string): string {
		const parts: string[] = [];
		if (s.obj) parts.push(s.obj.fields.size ? iface(hint, s.obj) : 'Record<string, unknown>');
		if (s.arr) {
			const el = s.arr.items ? ts(s.arr.items, `${hint}Item`) : 'unknown';
			parts.push(el.includes('|') ? `(${el})[]` : `${el}[]`);
		}
		if (s.prims.has('string')) parts.push('string');
		if (s.prims.has('integer') || s.prims.has('number')) parts.push('number');
		if (s.prims.has('boolean')) parts.push('boolean');
		if (s.prims.has('null')) parts.push('null');
		return parts.length ? parts.join(' | ') : 'unknown';
	}

	const root = infer(value);
	if (root.obj && root.obj.fields.size && root.prims.size === 0 && !root.arr) iface(name, root.obj);
	else {
		used.add(name);
		const body = ts(root, name);
		blocks.unshift(`export type ${name} = ${body};`); // the root comes first
	}
	return `// inferred from one request, best effort\n${blocks.join('\n\n')}\n`;
}

type Schema = Record<string, unknown>;

function schemaOf(s: Shape): Schema {
	const variants: Schema[] = [];
	const types: string[] = [];
	if (s.obj) {
		const obj = s.obj;
		const properties: Record<string, Schema> = {};
		const required: string[] = [];
		for (const [k, f] of obj.fields) {
			properties[k] = schemaOf(f.shape);
			if (f.count === obj.seen) required.push(k);
		}
		const o: Schema = { type: 'object', properties };
		if (required.length) o.required = required;
		variants.push(o);
	}
	if (s.arr) variants.push(s.arr.items ? { type: 'array', items: schemaOf(s.arr.items) } : { type: 'array' });
	if (s.prims.has('string')) types.push('string');
	if (s.prims.has('number')) types.push('number');
	else if (s.prims.has('integer')) types.push('integer');
	if (s.prims.has('boolean')) types.push('boolean');
	if (s.prims.has('null')) types.push('null');

	// Fold plain types into a single object/array schema as a type list when we can.
	if (variants.length === 1 && types.length) {
		const v = variants[0];
		return { ...v, type: [v.type as string, ...types] };
	}
	if (types.length) variants.push({ type: types.length === 1 ? types[0] : types });
	if (variants.length === 0) return {};
	return variants.length === 1 ? variants[0] : { anyOf: variants };
}

/** A JSON Schema (draft 2020-12) for the value. */
export function toJSONSchema(value: unknown, title = 'WebhookPayload'): string {
	const schema = {
		$schema: 'https://json-schema.org/draft/2020-12/schema',
		$comment: 'inferred from one request, best effort',
		title,
		...schemaOf(infer(value))
	};
	return JSON.stringify(schema, null, 2) + '\n';
}
