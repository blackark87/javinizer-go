import { describe, expect, it, vi } from 'vitest';
import { shouldToggleCardFromClick, shouldToggleCardFromKey } from './card-selection';

describe('actress card selection', () => {
	it('toggles for non-interactive card content', () => {
		expect(shouldToggleCardFromClick({ closest: vi.fn().mockReturnValue(null) } as never)).toBe(
			true,
		);
	});

	it('does not double-toggle links, buttons, or checkboxes', () => {
		expect(
			shouldToggleCardFromClick({
				closest: vi.fn().mockReturnValue({ tagName: 'INPUT' }),
			} as never),
		).toBe(false);
	});

	it('supports Enter and Space only on the card itself', () => {
		const card = {};
		expect(
			shouldToggleCardFromKey({
				key: 'Enter',
				target: card,
				currentTarget: card,
			} as KeyboardEvent),
		).toBe(true);
		expect(
			shouldToggleCardFromKey({
				key: ' ',
				target: card,
				currentTarget: card,
			} as KeyboardEvent),
		).toBe(true);
		expect(
			shouldToggleCardFromKey({
				key: 'Enter',
				target: {},
				currentTarget: card,
			} as KeyboardEvent),
		).toBe(false);
	});
});
