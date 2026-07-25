import { describe, expect, it } from 'vitest';
import {
	completedActressName,
	completedContentMetadataURL,
	completedContentPageCount,
	completedContentSearchURL,
	completedContentTitle,
} from './completed-utils';

describe('completed content utilities', () => {
	it('uses the most useful available title', () => {
		expect(
			completedContentTitle({
				movie_id: 'MIUM-985',
				display_title: '표시 제목',
				title: '제목',
				actresses: [],
				paths: [],
				latest_job_id: 'job-1',
				organized_at: '2026-07-26T00:00:00Z',
			}),
		).toBe('표시 제목');
		expect(
			completedContentTitle({
				movie_id: 'MIUM-985',
				actresses: [],
				paths: [],
				latest_job_id: 'job-1',
				organized_at: '2026-07-26T00:00:00Z',
			}),
		).toBe('MIUM-985');
	});

	it('prefers the Korean actress translation', () => {
		expect(
			completedActressName({
				first_name: 'Tomo',
				last_name: 'Shiraiwa',
				japanese_name: '白岩冬萌',
				translations: [{ language: 'ko', display_name: '시라이와 토모' }],
			}),
		).toBe('시라이와 토모');
	});

	it('builds stable search and pagination values', () => {
		expect(completedContentPageCount(41, 20)).toBe(3);
		expect(completedContentPageCount(0, 20)).toBe(1);
		expect(completedContentSearchURL(' 白岩冬萌 ', 2)).toBe(
			'/completed?q=%E7%99%BD%E5%B2%A9%E5%86%AC%E8%90%8C&page=2',
		);
		expect(completedContentSearchURL('  ', 1)).toBe('/completed');
	});

	it('builds an encoded cached-movie metadata route', () => {
		expect(completedContentMetadataURL('MIUM 985')).toBe('/movies/MIUM%20985');
	});
});
