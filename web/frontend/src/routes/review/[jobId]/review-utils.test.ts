import { describe, it, expect } from 'vitest';
import { computeCropPreview, resolvePosterUrl } from './review-utils';
import type { PosterCropBox, PosterPreviewOverride } from './review-utils';

function box(width: number, height: number): PosterCropBox {
	return { x: 0, y: 0, width, height };
}

describe('computeCropPreview', () => {
	it('returns empty output when crop box is null', () => {
		expect(computeCropPreview(null, 0)).toEqual({
			outputWidth: 0,
			outputHeight: 0,
			ratioLabel: '',
			willResize: false,
		});
	});

	it('preserves source resolution when max height is 0 (no cap)', () => {
		// Issue #33 regression: high-res crop must not be downscaled to 500
		const result = computeCropPreview(box(1032, 1468), 0);
		expect(result.outputWidth).toBe(1032);
		expect(result.outputHeight).toBe(1468);
		expect(result.willResize).toBe(false);
		// gcd(1032,1468)=4 → 258:367 exceeds 20, so decimal form is used.
		expect(result.ratioLabel).toBe('0.703:1');
	});

	it('simplifies 2:3 aspect ratio label', () => {
		// 800x1200 → gcd=400, 2:3
		const result = computeCropPreview(box(800, 1200), 0);
		expect(result.outputWidth).toBe(800);
		expect(result.outputHeight).toBe(1200);
		expect(result.willResize).toBe(false);
		expect(result.ratioLabel).toBe('2:3');
	});

	it('downscales preserving aspect ratio when source exceeds cap', () => {
		// 1032x1468 cap at 500 → 351x500
		const result = computeCropPreview(box(1032, 1468), 500);
		expect(result.outputWidth).toBe(351);
		expect(result.outputHeight).toBe(500);
		expect(result.willResize).toBe(true);
	});

	it('does not downscale when source equals the cap', () => {
		const result = computeCropPreview(box(800, 500), 500);
		expect(result.outputWidth).toBe(800);
		expect(result.outputHeight).toBe(500);
		expect(result.willResize).toBe(false);
	});

	it('does not downscale when source is below the cap', () => {
		const result = computeCropPreview(box(400, 600), 1000);
		expect(result.outputWidth).toBe(400);
		expect(result.outputHeight).toBe(600);
		expect(result.willResize).toBe(false);
	});

	it('uses decimal ratio when integers do not simplify small', () => {
		// 472x600 → gcd=8, 59:75 → both > 20? 59 > 20 → decimals
		const result = computeCropPreview(box(472, 600), 0);
		expect(result.outputWidth).toBe(472);
		expect(result.outputHeight).toBe(600);
		expect(result.willResize).toBe(false);
		// 472/600 = 0.787 → '0.787:1'
		expect(result.ratioLabel).toBe('0.787:1');
	});

	it('caps at 1 produce minimal output', () => {
		// 200x300 cap 1 → 1x1 (rounded)
		const result = computeCropPreview(box(200, 300), 1);
		expect(result.outputHeight).toBe(1);
		expect(result.outputWidth).toBe(1); // Math.round(200/300) = 1
		expect(result.willResize).toBe(true);
	});
});

describe('resolvePosterUrl', () => {
	const filePath = '/path/to/GOOD-001.mp4';
	const noSession = () => null;
	const withSession = () => 'sid-abc';

	function overrides(entries: [string, PosterPreviewOverride][] = []): Map<string, PosterPreviewOverride> {
		return new Map(entries);
	}

	it('returns undefined when no override, cropped_poster_url, or poster_url is set', () => {
		expect(resolvePosterUrl({}, filePath, overrides(), noSession)).toBeUndefined();
	});

	it('prefers cropped_poster_url over poster_url', () => {
		const movie = {
			poster_url: 'https://dmm/poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1',
		};
		expect(resolvePosterUrl(movie, filePath, overrides(), noSession)).toBe(
			'/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1',
		);
	});

	it('falls back to poster_url when cropped_poster_url is empty', () => {
		const movie = { poster_url: 'https://dmm/poster.jpg' };
		expect(resolvePosterUrl(movie, filePath, overrides(), noSession)).toBe('https://dmm/poster.jpg');
	});

	it('appends session query param to /api/v1/ URLs when a session ID is present (desktop WKWebView auth)', () => {
		// Regression: without the session param, the protected temp-poster
		// endpoint 401s in the desktop app (WKWebView doesn't persist cookies
		// for the Wails asset scheme) and the poster <img> shows "No Poster".
		const movie = { cropped_poster_url: '/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1' };
		const resolved = resolvePosterUrl(movie, filePath, overrides(), withSession);
		expect(resolved).toContain('session=sid-abc');
		// Only one '?' separator (no duplicated '?' corrupting the query string).
		expect((resolved?.match(/\?/g) ?? []).length).toBe(1);
	});

	it('uses & (not ?) when appending session to a URL that already has a query string', () => {
		// Regression: the crop modal built URLs with a duplicated '?'
		// (...?session=abc?v=123), corrupting the session value. resolvePosterUrl
		// must use the correct separator.
		const movie = { cropped_poster_url: '/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1' };
		const resolved = resolvePosterUrl(movie, filePath, overrides(), withSession);
		expect(resolved).toBe('/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1&session=sid-abc');
	});

	it('does not append session to external (non-/api/v1/) URLs', () => {
		// External URLs are proxied through getPreviewImageURL() elsewhere,
		// which appends session itself — appending here would double-tag.
		const movie = { poster_url: 'https://dmm/poster.jpg' };
		const resolved = resolvePosterUrl(movie, filePath, overrides(), withSession);
		expect(resolved).toBe('https://dmm/poster.jpg');
		expect(resolved).not.toContain('session=');
	});

	it('does not append session when no session ID is available (non-desktop / browser context)', () => {
		const movie = { cropped_poster_url: '/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1' };
		const resolved = resolvePosterUrl(movie, filePath, overrides(), noSession);
		expect(resolved).toBe('/api/v1/temp/posters/job-1/GOOD-001.jpg?v=1');
		expect(resolved).not.toContain('session=');
	});

	it('uses the override URL when present, with a cache-busting v= param', () => {
		const movie = { poster_url: 'https://dmm/original.jpg' };
		const ov = overrides([[filePath, { url: 'https://dmm/edited.jpg', version: 3 }]]);
		expect(resolvePosterUrl(movie, filePath, ov, noSession)).toBe('https://dmm/edited.jpg?v=3');
	});

	it('does not double-append v= when the override URL already has a v= param', () => {
		const movie = { poster_url: 'https://dmm/original.jpg' };
		const ov = overrides([[filePath, { url: 'https://dmm/edited.jpg?v=2', version: 3 }]]);
		expect(resolvePosterUrl(movie, filePath, ov, noSession)).toBe('https://dmm/edited.jpg?v=2');
	});

	it('appends session to an override /api/v1/ URL using the correct separator', () => {
		const movie = { poster_url: 'https://dmm/original.jpg' };
		const ov = overrides([[filePath, { url: '/api/v1/temp/posters/job-1/GOOD-001.jpg', version: 3 }]]);
		const resolved = resolvePosterUrl(movie, filePath, ov, withSession);
		// override adds ?v=3, then session appended with &.
		expect(resolved).toBe('/api/v1/temp/posters/job-1/GOOD-001.jpg?v=3&session=sid-abc');
		expect((resolved?.match(/\?/g) ?? []).length).toBe(1);
	});
});
