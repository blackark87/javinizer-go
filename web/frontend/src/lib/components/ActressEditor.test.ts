import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import ActressEditor from './ActressEditor.svelte';
import QueryClientWrapper from './QueryClientWrapper.svelte';
import type { Movie } from '$lib/api/types';

vi.mock('$lib/api/client', () => ({
	apiClient: {
		getConfig: vi.fn().mockResolvedValue({
			output: { first_name_order: false, actress_language_ja: false },
			metadata: { nfo: { actress_language_ja: false } },
		}),
		getPreviewImageURL: vi.fn((url: string) => `/api/v1/temp/image?url=${encodeURIComponent(url)}`),
		request: vi.fn().mockResolvedValue([]),
		actresses: {
			getAliasGroup: vi.fn().mockRejectedValue(new Error('no alias group')),
		},
	},
}));

if (!Element.prototype.animate) {
	// jsdom does not implement Web Animations, which Svelte transitions use.
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(Element.prototype as any).animate = function () {
		const animation = {
			onfinish: null as (() => void) | null,
			oncancel: null as (() => void) | null,
			effect: null,
			playState: 'finished' as const,
			currentTime: 0,
			cancel() {},
			finish() {
				animation.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		queueMicrotask(() => animation.onfinish?.());
		return animation;
	};
}

function renderEditor() {
	const thumbURL = 'https://pics.dmm.co.jp/mono/actjpgs/mihama_yui.jpg';
	const movie: Movie = {
		id: 'JNT-TEST',
		title: 'Test',
		actresses: [
			{
				id: 1,
				dmm_id: 123,
				first_name: '유이',
				last_name: '미하마',
				japanese_name: '三浜唯',
				thumb_url: thumbURL,
			},
		],
	};

	return {
		thumbURL,
		...render(
			ActressEditor,
			{ movie, onUpdate: vi.fn() },
			{
				wrapper: QueryClientWrapper,
				wrapperProps: {
					client: new QueryClient({ defaultOptions: { queries: { retry: false } } }),
				},
			},
		),
	};
}

describe('ActressEditor thumbnail previews', () => {
	it('uses the temporary image proxy for the card and edit preview', async () => {
		const { container, getAllByRole, getByText, thumbURL } = renderEditor();
		const proxiedURL = `/api/v1/temp/image?url=${encodeURIComponent(thumbURL)}`;

		await waitFor(() => {
			const cardImage = container.querySelector('img');
			expect(cardImage?.getAttribute('src')).toBe(proxiedURL);
		});

		await fireEvent.click(getAllByRole('button')[1]);
		expect(getByText('Edit Actress')).toBeTruthy();

		await waitFor(
			() => {
				const images = document.body.querySelectorAll('img');
				expect(Array.from(images).some((image) => image.getAttribute('src') === proxiedURL)).toBe(
					true,
				);
			},
			{ timeout: 1000 },
		);
	});
});
