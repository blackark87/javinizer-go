import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import TranslationSettingsSection from './TranslationSettingsSection.svelte';
import type { SettingsConfig } from '$lib/api/types';

if (!Element.prototype.animate) {
	Element.prototype.animate = vi.fn(() => ({
		cancel: vi.fn(), finish: vi.fn(), play: vi.fn(), pause: vi.fn(),
		onfinish: null, oncancel: null, effect: null, currentTime: 0,
	})) as unknown as typeof Element.prototype.animate;
}

function makeConfig(enableThinking: boolean): SettingsConfig {
	return {
		metadata: {
			translation: {
				enabled: true,
				provider: 'openai-compatible',
				openai_compatible: {
					base_url: 'http://localhost:1234/v1',
					api_key: '',
					model: 'gemma',
					enable_thinking: enableThinking,
					thinking_mode: 'boolean',
				},
			} as never,
		} as never,
	} as unknown as SettingsConfig;
}

describe('TranslationSettingsSection thinking mode', () => {
	it('stores the selected effort when thinking is enabled', async () => {
		const config = makeConfig(true);
		const { container } = render(TranslationSettingsSection, {
			config,
			inputClass: 'input',
			fetchTranslationModels: vi.fn(),
			fetchingTranslationModels: false,
			translationModelOptions: [],
		});
		const header = container.querySelector('button[aria-expanded="false"]') as HTMLButtonElement;
		await fireEvent.click(header);
		await waitFor(() => expect(container.querySelector('#translation-thinking-mode')).toBeTruthy());
		const select = container.querySelector('#translation-thinking-mode') as HTMLSelectElement;
		expect(select).toBeTruthy();
		expect(select.disabled).toBe(false);
		await fireEvent.change(select, { target: { value: 'high' } });
		expect(config.metadata.translation?.openai_compatible?.thinking_mode).toBe('high');
	});

	it('disables effort selection when thinking is disabled', async () => {
		const { container } = render(TranslationSettingsSection, {
			config: makeConfig(false),
			inputClass: 'input',
			fetchTranslationModels: vi.fn(),
			fetchingTranslationModels: false,
			translationModelOptions: [],
		});
		const header = container.querySelector('button[aria-expanded="false"]') as HTMLButtonElement;
		await fireEvent.click(header);
		await waitFor(() => expect(container.querySelector('#translation-thinking-mode')).toBeTruthy());
		const select = container.querySelector('#translation-thinking-mode') as HTMLSelectElement;
		expect(select).toBeTruthy();
		expect(select.disabled).toBe(true);
	});
});
