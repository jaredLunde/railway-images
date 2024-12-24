import { URL } from "node:url";
import { createHmac } from "node:crypto";

export type ClientOptions = {
	/** The URL of your service */
	url: string;
	/** Your service API key */
	secretKey: string;
	/** If provided, URLs will be signed locally instead of via server */
	signatureSecretKey?: string;
};

export class RailwayImagesClient {
	private baseURL: URL;
	private secretKey: string;
	private signatureSecretKey?: string;

	constructor(options: ClientOptions) {
		if (!options.url) {
			throw new Error("URL is required");
		}
		this.baseURL = new URL(options.url);
		this.secretKey = options.secretKey;
		this.signatureSecretKey = options.signatureSecretKey;
	}

	private async fetch(path: string, init?: RequestInit) {
		const url = new URL(path, this.baseURL);
		const headers: HeadersInit = {
			...init?.headers,
			"x-api-key": this.secretKey,
		};

		return fetch(url.toString(), { ...init, headers });
	}

	async sign(path: string): Promise<string> {
		if (this.signatureSecretKey) {
			const url = new URL(path, this.baseURL);
			return signUrl(url, this.signatureSecretKey);
		}

		const response = await this.fetch(`/sign/${path}`);
		return response.text();
	}

	async get(key: string): Promise<Response> {
		const response = await this.fetch(`/files/${key}`);
		if (response.status !== 200) {
			throw new Error(`${response.status}: ${response.statusText}`);
		}
		return response;
	}

	async put(
		key: string,
		content: ReadableStream | Buffer | ArrayBuffer,
	): Promise<Response> {
		return this.fetch(`/files/${key}`, {
			method: "PUT",
			body: content,
		});
	}

	async delete(key: string): Promise<Response> {
		return this.fetch(`/files/${key}`, { method: "DELETE" });
	}

	async list(options: ListOptions = {}): Promise<ListResult> {
		const params = new URLSearchParams();

		if (options.limit) {
			params.set("limit", options.limit.toString());
		}
		if (options.startingAt) {
			params.set("starting_at", options.startingAt);
		}
		if (options.unlinked) {
			params.set("unlinked", "true");
		}

		const response = await this.fetch(`/files?${params.toString()}`);
		return response.json();
	}
}

export type ListOptions = {
	/** The maximum number of keys to return */
	limit?: number;
	/** The key to start listing from */
	startingAt?: string;
	/** If true, list unlinked (soft deleted) files */
	unlinked?: boolean;
};

export type ListResult = {
	/** The keys of the files */
	keys: string[];
	/** A URL to the next page of results */
	nextPage?: string;
	/** Whether or not there are more results */
	hasMore: boolean;
};

export function sign(key: string, secret: string): string {
	key = key.replace(/^\//, ""); // TrimPrefix equivalent
	const hmac = createHmac("sha256", secret);
	hmac.update(key);
	return hmac.digest("base64url"); // base64url is the URL-safe version
}

export function signUrl(url: URL, secret: string): string {
	const nextURI = new URL(url.toString());
	const path = nextURI.pathname;
	const p = path.replace(/^\/sign/, "");

	if (!p.startsWith("/files") && !p.startsWith("/serve")) {
		throw new Error("invalid path");
	}

	let signature = "";
	const query = new URLSearchParams(nextURI.search);

	if (p.startsWith("/serve")) {
		signature = sign(p.replace(/^\/serve/, ""), secret);
	}

	if (p.startsWith("/files")) {
		const expireAt = Date.now() + 60 * 60 * 1000; // 1 hour in milliseconds
		query.set("x-expire", expireAt.toString());
		nextURI.search = query.toString();
		signature = sign(`${p}:${expireAt}`, secret);
	}

	nextURI.pathname = p;
	query.set("x-signature", signature);
	nextURI.search = query.toString();
	return nextURI.toString();
}
