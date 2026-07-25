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
	if (url.startsWith('/') && !url.startsWith('//')) return url;

	return apiClient.getPreviewImageURL(url);
}

export interface PosterImageSource {
	poster_url?: string;
	cover_url?: string;
	cropped_poster_url?: string;
	original_poster_url?: string;
	original_cropped_poster_url?: string;
	original_cover_url?: string;
}

export function previewImageCandidates(...urls: Array<string | undefined>): string[] {
	const seen = new Set<string>();
	const candidates: string[] = [];

	for (const raw of urls) {
		const normalized = raw?.trim();
		if (!normalized || seen.has(normalized)) continue;
		seen.add(normalized);
		const preview = previewImageUrl(normalized);
		if (preview) candidates.push(preview);
	}

	return candidates;
}

// Persisted scraper URLs remain valid after a batch temp directory is removed,
// so they must be attempted before cropped /api/v1/temp/posters paths.
export function posterImageCandidates(source: PosterImageSource): string[] {
	return previewImageCandidates(
		source.poster_url,
		source.original_poster_url,
		source.cropped_poster_url,
		source.original_cropped_poster_url,
		source.cover_url,
		source.original_cover_url,
	);
}
