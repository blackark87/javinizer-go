import { apiClient } from '$lib/api/client';

const PREVIEW_IMAGE_PATH = '/api/v1/temp/image';

function isPreviewImageUrl(url: string): boolean {
	if (url.startsWith(PREVIEW_IMAGE_PATH)) return true;

	try {
		return new URL(url).pathname === PREVIEW_IMAGE_PATH;
	} catch {
		return false;
	}
}

export function previewImageUrl(url?: string): string | undefined {
	if (!url) return undefined;
	if (isPreviewImageUrl(url)) return url;
	if (url.startsWith('/')) return url;

	return apiClient.getPreviewImageURL(url);
}
