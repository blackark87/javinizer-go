import { describe, expect, it } from 'vitest';

import { posterImageCandidates, previewImageCandidates, previewImageUrl } from './image';

describe('previewImageUrl', () => {
	it('returns undefined for missing image URLs', () => {
		expect(previewImageUrl()).toBeUndefined();
		expect(previewImageUrl('')).toBeUndefined();
	});

	it('preserves local root-relative image URLs', () => {
		expect(previewImageUrl('/covers/local.jpg')).toBe('/covers/local.jpg');
	});

	it('preserves already proxied preview image URLs', () => {
		expect(previewImageUrl('/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fcover.jpg')).toBe(
			'/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fcover.jpg',
		);
		expect(previewImageUrl('https://app.example.com/api/v1/temp/image?url=x')).toBe(
			'https://app.example.com/api/v1/temp/image?url=x',
		);
	});

	it('proxies absolute and protocol-relative image URLs', () => {
		expect(previewImageUrl('https://example.com/cover.jpg')).toBe(
			'/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fcover.jpg',
		);
		expect(previewImageUrl('//pics.dmm.co.jp/digital/video/example/examplepl.jpg')).toBe(
			'/api/v1/temp/image?url=%2F%2Fpics.dmm.co.jp%2Fdigital%2Fvideo%2Fexample%2Fexamplepl.jpg',
		);
	});
});

describe('previewImageCandidates', () => {
	it('removes blank and duplicate sources while preserving fallback order', () => {
		expect(
			previewImageCandidates(
				' https://example.com/poster.jpg ',
				'',
				'https://example.com/poster.jpg',
				'/local/fallback.jpg',
			),
		).toEqual([
			'/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fposter.jpg',
			'/local/fallback.jpg',
		]);
	});
});

describe('posterImageCandidates', () => {
	it('tries persistent poster URLs before temporary cropped posters and covers', () => {
		expect(
			posterImageCandidates({
				poster_url: 'https://example.com/poster.jpg',
				cropped_poster_url: '/api/v1/temp/posters/job/MOVIE.jpg',
				cover_url: 'https://example.com/cover.jpg',
			}),
		).toEqual([
			'/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fposter.jpg',
			'/api/v1/temp/posters/job/MOVIE.jpg',
			'/api/v1/temp/image?url=https%3A%2F%2Fexample.com%2Fcover.jpg',
		]);
	});
});
