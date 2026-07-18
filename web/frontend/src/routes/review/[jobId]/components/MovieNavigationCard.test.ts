import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/svelte';
import MovieNavigationCard from './MovieNavigationCard.svelte';

function renderCard(currentMovieIndex: number, onNavigate = vi.fn()) {
	return {
		...render(MovieNavigationCard, {
			props: {
				currentMovieIndex,
				movieResultsLength: 3,
				currentMovieId: `MOV-${currentMovieIndex + 1}`,
				hasChanges: false,
				onExclude: vi.fn(),
				onNavigate,
			},
		}),
		onNavigate,
	};
}

describe('MovieNavigationCard', () => {
	it('moves to the previous and next movie', async () => {
		const previous = renderCard(1);
		await fireEvent.click(previous.getByRole('button', { name: /Previous/ }));
		expect(previous.onNavigate).toHaveBeenCalledWith(0);
		previous.unmount();

		const next = renderCard(1);
		await fireEvent.click(next.getByRole('button', { name: /Next/ }));
		expect(next.onNavigate).toHaveBeenCalledWith(2);
	});

	it('moves to the selected page and disables boundary buttons', async () => {
		const first = renderCard(0);
		expect((first.getByRole('button', { name: /Previous/ }) as HTMLButtonElement).disabled).toBe(
			true,
		);
		await fireEvent.change(first.getByLabelText('Page'), { target: { value: '3' } });
		expect(first.onNavigate).toHaveBeenCalledWith(2);
		first.unmount();

		const last = renderCard(2);
		expect((last.getByRole('button', { name: /Next/ }) as HTMLButtonElement).disabled).toBe(true);
	});
});
