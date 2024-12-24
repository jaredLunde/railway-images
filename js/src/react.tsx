import type { Atom, ExtractAtomValue, PrimitiveAtom } from "jotai";
import { createStore } from "jotai";
import {
	atom,
	useAtomValue,
	useSetAtom,
	Provider as JotaiProvider,
} from "jotai";
import {
	createContext,
	useContext,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";

const RailwayImagesContext = createContext<RailwayImagesContextType>({});
type RailwayImagesContextType = {
	maxFileSize?: number;
	endpoints?: {
		get?: string;
		put?: string;
		sign?: string;
	};
};
const store = createStore();

export function Provider({
	children,
	...props
}: RailwayImagesContextType & { children: React.ReactNode }) {
	return (
		<RailwayImagesContext.Provider value={props}>
			<JotaiProvider store={store}>{children}</JotaiProvider>
		</RailwayImagesContext.Provider>
	);
}

export function Image(props: ImageProps) {
	const ctx = useContext(RailwayImagesContext);
}

type ImageProps = {
	format?: ImageFormat;
	size?: number | { width: number; height: number };
};

type ImageFormat = "jpeg" | "png" | "webp" | "avif";

export const IMAGE_MIMES = "image/*";

/**
 * A hook that returns a callback for selecting files from the browser dialog
 * and adding them to an array of pending files at a given ID.
 *
 * @param options - Select file options
 */
export function useSelectFiles(
	options: {
		/**
		 * Sets or retrieves a comma-separated list of content types.
		 */
		accept?: string;
		/**
		 * Sets or retrieves the `Boolean` value indicating whether multiple items
		 * can be selected from a list.
		 */
		multiple?: boolean;
		/**
		 * Called after the selected file atoms have been created
		 */
		onSelect?: (file: UploaderFileAtom) => void | Promise<void>;
	} = {},
): SelectFilesCallback {
	const storedOptions = useRef(options);
	useEffect(() => {
		storedOptions.current = options;
	});

	return useCallback(function selectFiles({ key } = {}) {
		// Create virtual input element
		const el = document.createElement("input");
		el.type = "file";
		el.multiple = storedOptions.current.multiple ?? true;

		if (storedOptions.current.accept) {
			el.accept = storedOptions.current.accept;
		}

		const onChange: EventListener = async (e) => {
			if (e.target instanceof HTMLInputElement) {
				const files: UploaderFileAtom[] = [];
				const target = e.target;

				for (const fileIndex in target.files) {
					const index = Number(fileIndex);

					if (isNaN(Number(index))) {
						continue;
					}

					const file = target.files.item(index);
					if (file === null) {
						continue;
					}

					const k = typeof key === "function" ? key(file) : key;
					const data = {
						id: crypto.randomUUID(),
						key: `${(k ?? (file.webkitRelativePath || file.name)).replace(/^\//, "")}`,
						file,
						source: URL.createObjectURL(file),
					};

					const bytesUploadedAtom = atom(0);
					const fileAtom = atom<UploaderFile>({
						...data,
						bytesUploaded: bytesUploadedAtom,
						progress: atom((get) => {
							return get(bytesUploadedAtom) / file.size;
						}),
						status: atom<ExtractAtomValue<UploaderFile["status"]>>("idle"),
						abortController: new AbortController(),
					});

					files.push(fileAtom);
					storedOptions.current.onSelect?.(fileAtom);
				}
				// Remove event listener after operation
				el.removeEventListener("change", onChange);
				// Remove input element after operation
				el.remove();
			}
		};

		el.addEventListener("change", onChange);
		el.click();
	}, []);
}

/**
 * A hook that returns a callback for selecting a directory from the browser dialog
 * and adding its contents to an array of pending files at a given ID.
 */
export function useSelectDirectory(
	options: {
		/**
		 * Called after the selected file atoms have been created
		 */
		onSelect?: (file: UploaderFileAtom) => void | Promise<void>;
	} = {},
): SelectDirectoryCallback {
	const storedOptions = useRef(options);
	useEffect(() => {
		storedOptions.current = options;
	});

	return useCallback(function selectDirectory({ key } = {}) {
		// Create virtual input element
		const el = document.createElement("input");
		el.type = "file";
		el.webkitdirectory = true;

		// eslint-disable-next-line func-style
		const onChange: EventListener = async (e) => {
			if (e.target instanceof HTMLInputElement) {
				const files: UploaderFileAtom[] = [];
				const target = e.target;

				for (const fileIndex in target.files) {
					const index = Number(fileIndex);

					if (isNaN(Number(index))) {
						continue;
					}

					// Get file object
					const file = target.files.item(index);

					if (file === null) {
						continue;
					}

					const k = typeof key === "function" ? key(file) : key;
					const data = {
						id: crypto.randomUUID(),
						key: `${(k ?? file.webkitRelativePath).replace(/^\//, "")}`,
						file,
						source: URL.createObjectURL(file),
					};

					const bytesUploadedAtom = atom(0);
					const fileAtom = atom<UploaderFile>({
						...data,
						bytesUploaded: bytesUploadedAtom,
						progress: atom((get) => {
							return get(bytesUploadedAtom) / file.size;
						}),
						status: atom<ExtractAtomValue<UploaderFile["status"]>>("idle"),
						abortController: new AbortController(),
					});

					files.push(fileAtom);
					storedOptions.current.onSelect?.(fileAtom);
				}

				// Remove event listener after operation
				el.removeEventListener("change", onChange);
				// Remove input element after operation
				el.remove();
			}
		};

		el.addEventListener("change", onChange);
		el.click();
	}, []);
}

/**
 * A hook that returns a callback for cancelling a file upload if
 * possible.
 *
 * @param atom - A file atom
 */
export function useCancelFileUpload(atom: UploaderFileAtom): () => void {
	const file = useAtomValue(atom, { store });
	const setStatus = useSetAtom(file.status, { store });
	return useCallback(() => {
		file.abortController.abort();
		setStatus("cancelled");
	}, [setStatus, file.abortController]);
}

/**
 * A hook that returns the `name`, `size`, `source`, and `id` from a
 * file atom
 *
 * @param atom - A file atom
 */
export function useFileData(
	atom: PrimitiveAtom<UploaderFile>,
): UploaderFileData {
	const file = useAtomValue(atom, { store });
	return useMemo(
		() => ({
			id: file.id,
			key: file.key,
			file: file.file,
			source: file.source,
		}),
		[file.id, file.key, file.file, file.source],
	);
}

/**
 * A hook that returns the status from a file atom
 *
 * @param atom - A file atom
 */
export function useFileStatus(
	atom: UploaderFileAtom,
): ExtractAtomValue<UploaderFile["status"]> {
	return useAtomValue(useAtomValue(atom, { store }).status, { store });
}

/**
 * A hook that returns the upload progress from a file atom if bytes uploaded
 * has been set by you.
 *
 * @param atom - A file atom
 * @example
 * ```tsx
 * const progress = useProgress(fileAtom);
 * return <span>{progress * 100}% uploaded</span>
 * ```
 */
export function useProgress(atom: UploaderFileAtom): number {
	return useAtomValue(useAtomValue(atom, { store }).progress, { store });
}

/**
 * A hook that returns a callback for uploading a file to the server
 *
 * @example
 * ```tsx
 *  const uploadFile = useUploadFile();
 *  ...
 *  uploadFile(file)
 * ```
 */
export function useUploadFile() {
	const ctx = useContext(RailwayImagesContext);
	return useCallback(async function uploadFile(
		file: UploaderFileAtom,
		options: UploadFileOptions = {},
	) {
		const { onProgress, onCancel, onSuccess, onError } = options;
		const { get, set } = store;
		const f = get(file);
		const uploadingFile = get(file);
		if (get(uploadingFile.status) === "cancelled") {
			return;
		}

		set(uploadingFile.status, "uploading");
		// If we catch an abort make sure the upload status has been changed to
		// cancel
		const abortSignal = f.abortController.signal;
		// eslint-disable-next-line func-style
		const handleAbortSignal = (): void => {
			onCancel?.();
			set(uploadingFile.status, "cancelled");
			abortSignal.removeEventListener("abort", handleAbortSignal);
		};
		abortSignal.addEventListener("abort", handleAbortSignal);
		let response: Response;

		// Bails out if we have aborted in the meantime
		if (
			f.abortController.signal.aborted ||
			get(uploadingFile.status) === "cancelled"
		) {
			return;
		}

		try {
			response = await new Promise((resolve, reject) => {
				const xhr = new XMLHttpRequest();
				abortSignal.addEventListener("abort", () => {
					xhr.abort();
					reject(new DOMException("Aborted", "AbortError"));
				});
				xhr.upload.addEventListener("progress", (e) => {
					if (e.lengthComputable) {
						set(f.bytesUploaded, e.loaded);
						onProgress?.(get(f.progress));
					}
				});
				xhr.addEventListener("load", () => {
					resolve(
						new Response(xhr.response, {
							status: xhr.status,
							statusText: xhr.statusText,
							headers: parseHeaders(xhr.getAllResponseHeaders()),
						}),
					);
				});
				xhr.addEventListener("error", () => reject(new Error("Upload failed")));
				xhr.open("PUT", joinPath(ctx.endpoints?.put ?? "", f.key));
				xhr.send(f.file);
			});

			if (!response.ok) {
				try {
					const responseText = await response.text();
					throw responseText;
				} catch (e) {
					throw `${response.status}: ${response.statusText}`;
				}
			}
		} catch (err) {
			set(uploadingFile.status, "error");
			const error =
				typeof err === "string"
					? err
					: err instanceof Error
						? err.message
						: "An unknown error occurred";
			set(file, (current) => ({ ...current, error }));
			onError?.(err);
		} finally {
			abortSignal.removeEventListener("abort", handleAbortSignal);
		}

		if (get(uploadingFile.status) === "uploading") {
			set(uploadingFile.status, "success");
			set(file, (current) => ({ ...current, response }));
			onSuccess?.(response!);
		}
	}, []);
}

function joinPath(base: string, path: string) {
	if (!base) return path;
	if (!path) return base;

	try {
		// Try parsing base as a full URL first
		const baseUrl = new URL(base);
		baseUrl.pathname = `${baseUrl.pathname}/${path}`.replace(/\/{2,}/g, "/");
		return baseUrl.toString();
	} catch {
		// If base isn't a valid URL, treat it as a path
		const u = new URL(
			typeof window === "undefined" ? "http://localhost" : window.location.href,
		);
		u.pathname = `${base}/${path}`.replace(/\/{2,}/g, "/");
		return u.pathname; // Return just the path portion
	}
}

function parseHeaders(headerStr: string) {
	const headers = new Headers();
	if (!headerStr) return headers;

	// Split into lines and filter out empty ones
	const headerPairs = headerStr.trim().split(/[\r\n]+/);

	headerPairs.forEach((line) => {
		const parts = line.split(": ");
		const key = parts.shift();
		const value = parts.join(": "); // Rejoin in case value contained ': '
		if (key && value) {
			headers.append(key.trim(), value.trim());
		}
	});

	return headers;
}

export function usePreviewUrl(file: UploaderFileAtom) {
	const [previewUrl, setPreviewUrl] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [status, setStatus] = useState<PreviewStatus>("idle");
	const clearPreview = useCallback(() => {
		setPreviewUrl(null);
		setError(null);
		setStatus("idle");
	}, []);

	useEffect(() => {
		const f = store.get(file);

		if (!f.file) {
			clearPreview();
			return;
		}

		setStatus("loading");
		setError(null);

		// Validate file type
		if (!f.file.type.startsWith("image/")) {
			setError("Selected file is not an image");
			setPreviewUrl(null);
			setStatus("error");
			return;
		}

		const reader = new FileReader();

		reader.onload = (e) => {
			if (
				e.target instanceof FileReader &&
				typeof e.target.result === "string"
			) {
				setPreviewUrl(e.target.result);
				setError(null);
				setStatus("success");
			}
		};

		reader.onerror = () => {
			setError("Error reading file");
			setPreviewUrl(null);
			setStatus("error");
		};

		reader.readAsDataURL(f.file);
		return () => {
			reader.abort();
			clearPreview();
		};
	}, [file, clearPreview]);

	return [
		previewUrl,
		useMemo(() => {
			return {
				error,
				status,
				clear: clearPreview,
			};
		}, [error, status, clearPreview]),
	] as const;
}

type PreviewStatus = "idle" | "loading" | "success" | "error";

type UploadFileOptions = {
	/**
	 * A function that is called when the upload is cancelled
	 */
	onCancel?: () => void;
	/**
	 * Called when all of the files have successfully uploaded
	 */
	onSuccess?: (responses: Response) => Promise<void> | void;
	/**
	 * Called when there is a progress event
	 */
	onProgress?: (progress: number) => Promise<void> | void;
	/**
	 * Called when there was an error uploading
	 */
	onError?: (err: unknown) => Promise<void> | void;
};

export type UploaderFilesAtomValue = {
	/**
	 * The ID used to create the atom with
	 */
	id: string;
	/**
	 * The files that have progressed through this atom in its lifetime
	 */
	files: PrimitiveAtom<UploaderFile>[];
};

export type UploaderFile = {
	/**
	 * A UUID for identifying the file
	 */
	id: string;
	/**
	 * The source of the file as a string if the file is less than 15MB in size,
	 * otherwise `null`. This is useful for generating previews.
	 */
	source: null | string;
	/**
	 * The path on the server to upload the file to
	 */
	key: string;
	/**
	 * The file
	 */
	file: File;
	/**
	 * A writable atom that contains the number of bytes that have been uploaded
	 * already (if updated by you, the developer)
	 */
	bytesUploaded: PrimitiveAtom<number>;
	/**
	 * A readonly atom containing the progress of the file upload if `bytesUploaded`
	 * has been set.
	 */
	progress: Atom<number>;
	/**
	 * An atom that stores the current status of the file:
	 * - `"idle"`: the file has not started uploading
	 * - `"queued"`: the file has been acknowledged and is waiting in a queue to upload
	 * - `"uploading"`: the file is uploading
	 * - `"cancelled"`: the file upload was cancelled by the user before it completed
	 * - `"success"`: the file has been successfully uploaded
	 * - `"error"`: an error occurred during the upload and it did not finish
	 */
	status: PrimitiveAtom<
		"idle" | "queued" | "uploading" | "cancelled" | "success" | "error"
	>;
	/**
	 * An error message if the status is in an error state
	 */
	error?: string;
	/**
	 * An abort controller signal that can be used to cancel the file upload
	 */
	abortController: AbortController;
};

export type UploaderFileAtom = PrimitiveAtom<UploaderFile>;

export type UploaderFileData = Pick<
	UploaderFile,
	"id" | "file" | "key" | "source"
>;

export type SelectFilesCallback = (options?: {
	/**
	 * The base path to upload to on the server-side. The file's name will
	 * be joined to this.
	 */
	key?: string | ((file: File) => string);
}) => void;

export type SelectDirectoryCallback = SelectFilesCallback;
