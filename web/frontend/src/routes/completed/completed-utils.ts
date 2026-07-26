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

export type CompletedContentSortValue =
	| 'organized_at_desc'
	| 'organized_at_asc'
	| 'metadata_created_at_desc'
	| 'metadata_created_at_asc'
	| 'metadata_updated_at_desc'
	| 'metadata_updated_at_asc';

export interface CompletedContentURLState {
	query?: string;
	actressID?: number;
	page?: number;
	pageSize?: number;
	sort?: CompletedContentSortValue;
}

export function completedContentSearchURL(state: CompletedContentURLState): string {
	const params = new URLSearchParams();
	const normalizedQuery = state.query?.trim() ?? '';
	if (normalizedQuery) params.set('q', normalizedQuery);
	if (state.actressID && state.actressID > 0) params.set('actress', String(state.actressID));
	if (state.pageSize && state.pageSize !== 20) params.set('limit', String(state.pageSize));
	if (state.sort && state.sort !== 'organized_at_desc') params.set('sort', state.sort);
	if (state.page && state.page > 1) params.set('page', String(state.page));
	const suffix = params.toString();
	return suffix ? `/completed?${suffix}` : '/completed';
}

export function completedContentMetadataURL(movieID: string): string {
	return `/movies/${encodeURIComponent(movieID)}`;
}
