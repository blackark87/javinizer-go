import type { Actress, CompletedContentItem } from '$lib/api/types';
import { formatActressName } from '$lib/utils/actress';

export function completedContentTitle(content: CompletedContentItem): string {
	return (
		content.display_title ||
		content.title ||
		content.original_title ||
		content.movie_id ||
		'Unknown'
	);
}

export function completedActressName(actress: Actress): string {
	const korean = actress.translations?.find((translation) => translation.language === 'ko');
	return korean?.display_name || formatActressName(actress);
}

export function completedContentPageCount(total: number, pageSize: number): number {
	if (pageSize <= 0) return 1;
	return Math.max(1, Math.ceil(Math.max(0, total) / pageSize));
}

export function completedContentSearchURL(query: string, page: number): string {
	const params = new URLSearchParams();
	const normalizedQuery = query.trim();
	if (normalizedQuery) params.set('q', normalizedQuery);
	if (page > 1) params.set('page', String(page));
	const suffix = params.toString();
	return suffix ? `/completed?${suffix}` : '/completed';
}
