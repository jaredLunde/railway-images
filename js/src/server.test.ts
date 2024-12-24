import { describe, it, expect, vi } from "vitest";
import { RailwayImagesClient, sign, signUrl } from "./server";

describe("sign", () => {
	it("signs a key with secret", () => {
		const signature = sign("test.jpg", "secret");
		// Base64-encoded HMAC-SHA256 of "test.jpg" with key "secret"
		expect(signature).toBe("HqclqWcEibRp4SvI22S61tngoLJlpGfHgdyYjOaQ770");
	});

	it("trims leading slash", () => {
		const withSlash = sign("/test.jpg", "secret");
		const withoutSlash = sign("test.jpg", "secret");
		expect(withSlash).toBe(withoutSlash);
	});
});

describe("signUrl", () => {
	it("signs serve URL", () => {
		const url = new URL("http://example.com/serve/test.jpg");
		const signed = signUrl(url, "secret");
		const parsed = new URL(signed);
		expect(parsed.pathname).toBe("/serve/test.jpg");
		expect(parsed.searchParams.get("x-signature")).toBeTruthy();
	});

	it("signs files URL with expiration", () => {
		const url = new URL("http://example.com/files/test.jpg");
		const signed = signUrl(url, "secret");
		const parsed = new URL(signed);
		expect(parsed.pathname).toBe("/files/test.jpg");
		expect(parsed.searchParams.get("x-signature")).toBeTruthy();
		expect(parsed.searchParams.get("x-expire")).toBeTruthy();
	});

	it("throws on invalid path", () => {
		const url = new URL("http://example.com/invalid/test.jpg");
		expect(() => signUrl(url, "secret")).toThrow("invalid path");
	});
});

describe("RailwayImagesClient", () => {
	it("constructor validates URL", () => {
		expect(
			() => new RailwayImagesClient({ url: "", secretKey: "key" }),
		).toThrow("URL is required");
	});

	it("signs URLs locally when signatureSecretKey provided", async () => {
		const client = new RailwayImagesClient({
			url: "http://example.com",
			secretKey: "key",
			signatureSecretKey: "signing-key",
		});

		const signed = await client.sign("/files/test.jpg");
		expect(signed).toContain("x-signature=");
	});

	it("uses server signing when no signatureSecretKey", async () => {
		const client = new RailwayImagesClient({
			url: "http://example.com",
			secretKey: "key",
		});

		// Mock fetch
		global.fetch = vi
			.fn()
			.mockImplementation((url: string, init?: RequestInit) => {
				expect(url).toContain("/sign/");
				// @ts-expect-error
				expect(init?.headers?.["x-api-key"]).toBe("key");
				return Promise.resolve(new Response("signed-url"));
			});

		const signed = await client.sign("test.jpg");
		expect(signed).toBe("signed-url");
	});

	it("list constructs correct query params", async () => {
		const client = new RailwayImagesClient({
			url: "http://example.com",
			secretKey: "key",
		});

		global.fetch = vi.fn().mockImplementation((url: string) => {
			const parsed = new URL(url);
			expect(parsed.searchParams.get("limit")).toBe("10");
			expect(parsed.searchParams.get("starting_at")).toBe("start");
			expect(parsed.searchParams.get("unlinked")).toBe("true");
			return Promise.resolve(
				new Response(
					JSON.stringify({
						keys: ["test.jpg"],
						hasMore: false,
					}),
				),
			);
		});

		const result = await client.list({
			limit: 10,
			startingAt: "start",
			unlinked: true,
		});

		expect(result.keys).toEqual(["test.jpg"]);
		expect(result.hasMore).toBe(false);
	});

	it("handles error responses", async () => {
		const client = new RailwayImagesClient({
			url: "http://example.com",
			secretKey: "key",
		});

		global.fetch = vi.fn().mockImplementation(() => {
			return Promise.resolve(new Response("error message", { status: 500 }));
		});

		await expect(client.get("test.jpg")).rejects.toThrow("HTTP error 500");
	});
});
